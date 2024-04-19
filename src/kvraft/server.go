package kvraft

import (
	"fmt"

	"sync"
	"sync/atomic"
	"time"

	"6.5840/labgob"
	"6.5840/labrpc"
	"6.5840/raft"
)

const ExcuteTimeOut = time.Duration(500) * time.Millisecond //执行超时，使用在

// 提交到日志文件中的命令
type Command struct {
	Key         string
	Value       string
	ClientId    int64
	SequenceNum int64
	CommandOp   CommandOp
}

// KV状态机定义
type KVStateMachine interface {
	Put(key, value string) error
	Append(key, value string) error
	Get(key string) (value string, err error) //如果key不存在，则err != nil
}

// 基于内存的KV存储
type MemKV struct {
	kvMp map[string]string
}

func newMemKV() *MemKV {
	return &MemKV{
		kvMp: make(map[string]string),
	}
}

func (mkv *MemKV) Put(key string, value string) error {
	mkv.kvMp[key] = value
	return nil
}

func (mkv *MemKV) Append(key string, value string) error {
	mkv.kvMp[key] += value
	return nil
}

func (mkv *MemKV) Get(key string) (value string, err error) {
	if value, ok := mkv.kvMp[key]; ok {
		return value, nil
	}
	return "", fmt.Errorf(ErrNoKey)
}

// 命令结果
type CommandResult struct {
	sequenceNum int64  // 命令序列号
	status      status //如果状态机应用了命令，则返回OK
	response    string // 如果状态OK，回复状态机的输出
}

func (cr *CommandResult) String() string {
	return fmt.Sprintf("{SN:%d,STATUS:%s,R:%s}", cr.sequenceNum, cr.status, cr.response)
}

// 唤醒协程时的消息
type NotifyMsg struct {
	response string
	status   status
}

type KVServer struct {
	mu      sync.Mutex
	me      int
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg
	dead    int32 // set by Kill()

	maxraftstate int // snapshot if log grows this big

	lastApplied    int // 保证状态机不会回退
	KVstateMachine KVStateMachine
	//为ClientId记录最后一条执行的命令,注意只有Leader才会更新duplicateMp
	duplicateMp       map[int64]*CommandResult
	notifyChMp map[int]chan *NotifyMsg //用于唤醒对应日志中对应LogEntry

}

func StartKVServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int) *KVServer {
	// call labgob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	labgob.Register(Command{})
	kv := &KVServer{
		me:             me,
		maxraftstate:   maxraftstate,
		applyCh:        make(chan raft.ApplyMsg),
		KVstateMachine: newMemKV(),
		duplicateMp:           make(map[int64]*CommandResult),
		notifyChMp:     make(map[int]chan *NotifyMsg),
	}
	kv.rf = raft.Make(servers, me, persister, kv.applyCh)
	go kv.applier()
	return kv
}

// 判断请求的命令是否存在重复
func (kv *KVServer) isDuplicate(clientId int64, sequenceNum int64) bool {
	if _, ok := kv.duplicateMp[clientId]; !ok { //当前idMp中没有存储clientId
		return false
	} else { // 当前idMp中存储了clientId
		if sequenceNum <= kv.duplicateMp[clientId].sequenceNum {
			DPrintf(dServer, "KVS%d recevie %v and duplicate", kv.me, sequenceNum)
			return true
		}else{
			return false
		}
	}
}

// 更新idMp
func (kv *KVServer) updateDuplicateMp(clientId int64, sequenceNum int64, status status, response string) {
	if _, ok := kv.duplicateMp[clientId]; ok { //已经存在，原地修改内容
		kv.duplicateMp[clientId].sequenceNum = sequenceNum
		kv.duplicateMp[clientId].status = status
		kv.duplicateMp[clientId].response = response
	} else { //clientId不在表中，创建内容
		DPrintf(dServer,"KVS%d create C%d in duplicateMp",kv.me,clientId)
		kv.duplicateMp[clientId] = &CommandResult{
			sequenceNum: sequenceNum,
			status:      status,
			response:    response,
		}
	}
}

// 应用日志到状态机
func (kv *KVServer) applyLogToKVStateMachine(op Command) (string, error) {
	switch op.CommandOp {
	case GetOp:
		return kv.KVstateMachine.Get(op.Key)
	case PutOp:
		err := kv.KVstateMachine.Put(op.Key, op.Value)
		return "", err
	case AppendOp:
		err := kv.KVstateMachine.Append(op.Key, op.Value)
		return "", err
	default:
		panic(fmt.Sprintf("Unexcepted CommandOp %v\n", op.CommandOp))
	}
}

func (kv *KVServer) getNotifyCh(key int) chan *NotifyMsg {
	if ch, ok := kv.notifyChMp[key]; ok {
		return ch
	}
	//因为channel的接收端可能不会接收数据，因此必须至少带1缓冲，否则死锁
	kv.notifyChMp[key] = make(chan *NotifyMsg, 1)
	return kv.notifyChMp[key]
}

// 客户端调用Command RPC，修改状态机的状态
func (kv *KVServer) Command(args *CommandArgs, reply *CommandReply) {
	defer DPrintf(dServer, "KVS%d process args %v with reply %v", kv.me, args, reply)
	kv.lock("Command isDuplicate")
	if kv.isDuplicate(args.ClientId, args.SequenceNum) {
		reply.Status, reply.Response = kv.duplicateMp[args.ClientId].status, kv.duplicateMp[args.ClientId].response
		kv.unlock("Command isDuplicate")
		return
	}
	kv.unlock("Command isDuplicate")
	commandIndex, _, isLeader := kv.rf.Start(Command{Key: args.Key, Value: args.Value, CommandOp: args.Op,ClientId: args.ClientId,SequenceNum: args.SequenceNum})
	if !isLeader { //当前Server不是Leader
		reply.Status, reply.Response = ErrWrongLeader, ""
		return
	}
	kv.lock("Command GetNotifyCh")
	ch := kv.getNotifyCh(commandIndex)
	kv.unlock("Command GetNotifyCh")
	select {
	case rsp := <-ch:
		reply.Response, reply.Status = rsp.response, rsp.status
	case <-time.After(ExcuteTimeOut):
		reply.Response, reply.Status = "", ErrTimeOut
	}
	go func() {
		kv.lock("Command DeleteCh")
		defer kv.unlock("Command DeleteCh")
		delete(kv.notifyChMp, commandIndex)
	}()
}

func (kv *KVServer) applier() {
	for !kv.killed() {
		m := <-kv.applyCh
		if m.CommandValid { // 应用日志
			kv.lock("applier commandValid")
			if m.CommandIndex < kv.lastApplied { //因快照更新，m.CommandIndex < kv.lastApplied
				kv.unlock("applier m.CommandIndex < kv.lastApplied")
				continue
			}
			kv.lastApplied = m.CommandIndex
			var response string
			var status status
			command := m.Command.(Command)
			// 当网络不稳定时，前一个Leader已经让大部分节点都拥有了写日志
			// 但是还没来得及提交，前Leader就死了，然后新Leader上任，接到了同一请求
			// 于是日志中就存在了两个相同的请求，如果这两个都是写请求，就会破坏写一致性
			// 因此，在此处对日志中提交的命令进行判断，保证写一致性
			if kv.isDuplicate(command.ClientId,command.SequenceNum) {
				kv.unlock("applier kv.isDuplicate(command.ClientId,command.SequenceNum)")
				continue
			}
			if resp, err := kv.applyLogToKVStateMachine(command); err != nil { // 读请求
				response, status = resp, ErrNoKey
			} else { // 写请求
				response, status = resp, OK
			}
			kv.updateDuplicateMp(command.ClientId, command.SequenceNum, status, response) //更新clientIdMp
			if curTerm, isLeader := kv.rf.GetState(); isLeader && curTerm == m.CommandTerm { //当且仅当服务器本身是Leader，且是对应任期，才响应
				ch := kv.getNotifyCh(m.CommandIndex)
				ch <- &NotifyMsg{response: response, status: status}
			}
			kv.unlock("applier commandValid")
		} else if m.SnapshotValid {
			panic(fmt.Sprintf("Unimplemented %v", m))
		} else {
			panic(fmt.Sprintf("Unexcepted Error due to %v", m))
		}
	}
}

func (kv *KVServer) Kill() {
	atomic.StoreInt32(&kv.dead, 1)
	kv.rf.Kill()
	DPrintf(dServer, "KVS%d Killed", kv.me)
}

func (kv *KVServer) killed() bool {
	z := atomic.LoadInt32(&kv.dead)
	return z == 1
}

func (kv *KVServer) lock(s string) {
	// DPrintf(dServer, "KVS%d try lock during %s", kv.me, s)
	kv.mu.Lock()
	// DPrintf(dServer, "KVS%d get lock during %s", kv.me, s)
}

func (kv *KVServer) unlock(s string) {
	// DPrintf(dServer, "KVS%d try unlock during %s", kv.me, s)
	kv.mu.Unlock()
	// DPrintf(dServer, "KVS%d complete unlock during %s", kv.me, s)
}
