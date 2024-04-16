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

const ExcuteTimeOut = time.Duration(240) * time.Millisecond  //执行超时，使用在

// 提交到日志文件中的操作
type Op struct {
	// Your definitions here.
	// Field names must start with capital letters,
	// otherwise RPC will break.
	Key   string
	Value string
	CommandOp    CommandOp
}

//KV状态机定义
type KVStateMachine interface {
	Put(key,value string) error
	Append(key,value string) error
	Get(key string) (value string,err error) //如果key不存在，则err != nil
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

func (mkv *MemKV) Put(key string, value string) error{
	mkv.kvMp[key] = value
	return nil
}

func (mkv *MemKV) Append(key string, value string) error{
	mkv.kvMp[key] += value
	return nil
}

func (mkv *MemKV) Get(key string) (value string, err error) {
	if value,ok := mkv.kvMp[key];ok{
		return value,nil
	}
	return "",fmt.Errorf(ErrNoKey)
}

// 命令结果
type CommandResult struct{
	sequenceNum int64  // 命令序列号
	status     status //如果状态机应用了命令，则返回OK
	response string // 如果状态OK，回复状态机的输出
}

// 唤醒协程时的消息
type NotifyMsg struct {
	response string
	status status
}

type KVServer struct {
	mu      sync.Mutex
	me      int
	rf      *raft.Raft
	applyCh chan raft.ApplyMsg
	dead    int32 // set by Kill()

	maxraftstate int // snapshot if log grows this big
	
	lastApplied int // 保证状态机不会回退
	KVstateMachine KVStateMachine
	//为ClientId记录最后一条执行的命令,注意只有Leader才会更新idMp
	idMp map[int64]*CommandResult 
	notifyChMp map[int]chan *NotifyMsg //用于唤醒对应日志中对应LogEntry

}

func StartKVServer(servers []*labrpc.ClientEnd, me int, persister *raft.Persister, maxraftstate int) *KVServer {
	// call labgob.Register on structures you want
	// Go's RPC library to marshall/unmarshall.
	labgob.Register(Op{})
	kv := &KVServer{
		me:           me,
		maxraftstate: maxraftstate,
		applyCh:      make(chan raft.ApplyMsg),
		KVstateMachine: newMemKV(),
		idMp: make(map[int64]*CommandResult),
		notifyChMp: make(map[int]chan *NotifyMsg),
	}
	kv.rf = raft.Make(servers, me, persister, kv.applyCh)
	go kv.applier()
	return kv
}

// 判断请求的命令是否存在重复
func (kv *KVServer) isDuplicate(clientId int64,sequenceNum int64) bool {
	if _,ok:= kv.idMp[clientId];!ok{ //当前idMp中没有存储clientId
		return false
	}else{ // 当前idMp中存储了clientId
		DPrintf(dServer,"S%d recevie %v and duplicate",kv.me,sequenceNum)
		return sequenceNum <= kv.idMp[clientId].sequenceNum 
	}
	
}

// 更新idMp
func (kv *KVServer) updateIdMp(clientId int64,sequenceNum int64,status status,response string){
	if _,ok := kv.idMp[clientId];ok{ //已经存在，原地修改内容
		kv.idMp[clientId].sequenceNum = sequenceNum
		kv.idMp[clientId].status = status
		kv.idMp[clientId].response = response
	}else{ //clientId不在表中，创建内容
		kv.idMp[clientId] = &CommandResult{
			sequenceNum: sequenceNum,
			status: status,
			response: response,
		}
	}
}

// 应用日志到状态机
func (kv *KVServer) applyLogToKVStateMachine(op Op) (string,error){
	switch op.CommandOp {
	case GetOp:
		return kv.KVstateMachine.Get(op.Key)
	case PutOp:
		err := kv.KVstateMachine.Put(op.Key,op.Value)
		return "",err
	case AppendOp:
		err := kv.KVstateMachine.Append(op.Key,op.Value)
		return "",err
	default:
		panic(fmt.Sprintf("Unexcepted CommandOp %v\n",op.CommandOp))
	}
}

func (kv *KVServer) getNotifyCh(key int) chan *NotifyMsg{
	if ch,ok:= kv.notifyChMp[key];ok{
		return ch
	}
	kv.notifyChMp[key] = make(chan *NotifyMsg)
	return kv.notifyChMp[key]
}

// 客户端调用Command RPC，修改状态机的状态
func (kv *KVServer) Command(args *CommandArgs,reply *CommandReply){
	defer DPrintf(dServer,"S%d state %v",kv.me,kv.KVstateMachine)
	kv.mu.Lock()
	if kv.isDuplicate(args.ClientId,args.SequenceNum) {
		reply.Status,reply.Response = kv.idMp[args.ClientId].status,kv.idMp[args.ClientId].response
		kv.mu.Unlock()
		return 
	}
	kv.mu.Unlock()
	commandIndex,_,isLeader:= kv.rf.Start(Op{Key: args.Key,Value: args.Value,CommandOp:args.Op})
	if !isLeader { //当前Server不是Leader
		kv.mu.Lock()
		reply.Status,reply.Response = ErrWrongLeader,""
		kv.mu.Unlock()
		return
	}
	kv.mu.Lock()
	ch := kv.getNotifyCh(commandIndex)
	kv.mu.Unlock()
	select {
	case rsp := <- ch:
		reply.Response,reply.Status = rsp.response,rsp.status
	case <- time.After(ExcuteTimeOut):
		reply.Response,reply.Status = "",ErrTimeOut
	}
	go func() {
		kv.mu.Lock()
		defer kv.mu.Unlock()
		delete(kv.notifyChMp,commandIndex)
	}()
}

func (kv *KVServer) applier() {
	for !kv.killed() {
		m := <-kv.applyCh
		if m.CommandValid { // 应用日志
			kv.mu.Lock()
			// 如果底层applier重复提交，则过滤。
			// 不过在当前实现中，出现该情况
			if m.CommandIndex < kv.lastApplied { 
				kv.mu.Unlock()
				continue
			}
			kv.lastApplied = m.CommandIndex 
			
			var response string
			var status status
			op := m.Command.(Op)
			if resp,err:= kv.applyLogToKVStateMachine(op);err != nil{ // 读请求
				response,status = resp,ErrNoKey
			}else{
				response,status = resp,OK
			}
			if curTerm,isLeader := kv.rf.GetState(); 
				isLeader && curTerm == m.CommandTerm{//当且仅当服务器本身是Leader，且是对应任期，才响应
				ch := kv.getNotifyCh(m.CommandIndex)
				ch <- &NotifyMsg{response: response,status: status}
			}
			kv.mu.Unlock()
		} else if m.SnapshotValid {
			panic(fmt.Sprintf("Unimplemented %v",m))
		} else{
			panic(fmt.Sprintf("Unexcepted Error due to %v",m))
		}
	}
}

func (kv *KVServer) Kill() {
	atomic.StoreInt32(&kv.dead, 1)
	kv.rf.Kill()
	DPrintf(dServer,"KVS%d Killed", kv.me)
}

func (kv *KVServer) killed() bool {
	z := atomic.LoadInt32(&kv.dead)
	return z == 1
}