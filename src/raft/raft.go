package raft

import (
	"bytes"
	"sync"
	"sync/atomic"
	"time"

	"6.5840/labgob"
	"6.5840/labrpc"
)

type ApplyMsg struct {
	CommandValid bool        //如果命令已经被提交了，返回True
	Command      interface{} //要执行的命令
	CommandIndex int         //下标

	// For 2D:
	SnapshotValid bool
	Snapshot      []byte
	SnapshotTerm  int
	SnapshotIndex int
}

// 实现单个Raft节点的go结构体
type Raft struct {
	mu        sync.RWMutex        // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	//需要持久化的变量
	CurrentTerm int        //服务器已经见识过的任期（在启动时初始化为0，单调递增）
	VotedFor    int        //当前任期，本服务器投票的候选人id，（如果没有则为空，实际实现中如果没有我设为-1）
	Log         []LogEntry //日志条目；每个条目包含命令和从领导者收到条目的任期，索引从1开始
	//非持久化的变量
	state       RaftState     //表示当前节点状态
	commitIndex int           //已知的已提交的最高日志条目的索引（初始化为 0，单调递增）
	lastApplied int           //应用于状态机的最高日志条目的索引（初始化为 0，单调递增）
	applyCh     chan ApplyMsg //当日志条目提交时，向管道写入ApplyMsg
	applyCond   *sync.Cond    //用于应用日志
	//非持久化的Leader使用的变量
	nextIndex      []int        //对于每个服务器，要发送到该服务器的下一条日志条目的索引（初始化为领导者的最后一条日志索引 + 1）
	matchIndex     []int        //在每个服务器上已知已经复制的最高日志条目的索引（初始化为 0，单调递增）
	replicatorCond []*sync.Cond // 用于为每一个复制协程批处理复制条目
	// 计时器
	electionTimer  *time.Timer //选举计时器
	heartbeatTimer *time.Timer //心跳计时器
}

func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	defer Debug(dInfo, "S%d raft node created", me)
	rf := &Raft{
		peers:     peers,
		persister: persister,
		me:        me,
		// 初始化持久化的变量
		CurrentTerm: 0,
		VotedFor:    -1,
		Log:         make([]LogEntry, 1),
		// 初始化非持久化变量
		state:       FOLLOWER,
		commitIndex: 0,
		lastApplied: 0,
		applyCh:     applyCh,
		// 初始化Leader需要使用的遍历
		nextIndex:  make([]int, len(peers)),
		matchIndex: make([]int, len(peers)),
		// 计时器
		replicatorCond: make([]*sync.Cond, len(peers)),
		electionTimer:  time.NewTimer(randomizedElectionTimeout()),
		heartbeatTimer: time.NewTimer(HEARTBEATETIME),
	}
	rf.readPersist(persister.ReadRaftState())
	rf.applyCond = sync.NewCond(&rf.mu) // 应用条目的时候应当保证不会产生数据竞争，所以共用一把锁
	lastLog := rf.getLastLog()
	for i := 0; i < len(peers); i++ {
		rf.matchIndex[i], rf.nextIndex[i] = 0, lastLog.Index+1
		if i != rf.me {
			rf.replicatorCond[i] = sync.NewCond(&sync.Mutex{})
			go rf.replicator(i)
		}
	}
	go rf.ticker()
	go rf.applyTicker()
	return rf
}

// 获取Raft节点状态，加锁的方法
func (rf *Raft) GetState() (int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	term := rf.CurrentTerm
	isLeader := (rf.state == LEADER)
	return term, isLeader
}

// 返回日志的第一项条目
func (rf *Raft) getFirstLog() LogEntry {
	return rf.Log[0]
}

// 返回日志的最后一项条目
func (rf *Raft) getLastLog() LogEntry {
	lastIndex := len(rf.Log) - 1
	return rf.Log[lastIndex]
}

// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
// before you've implemented snapshots, you should pass nil as the
// second argument to persister.Save().
// after you've implemented snapshots, pass the current snapshot
// (or nil if there's not yet a snapshot).
func (rf *Raft) persist() {
	// TODO (2C) Your code here .
	// Example:
	// defer Debug(dLog, "S%d persist at T%d,votedFor:S%d,lastLog:{%v}", rf.me, rf.CurrentTerm, rf.VotedFor, rf.Log[len(rf.Log)-1])
	// w := new(bytes.Buffer)
	// e := labgob.NewEncoder(w)
	// e.Encode(rf.CurrentTerm)
	// e.Encode(rf.VotedFor)
	// e.Encode(rf.Log)
	// raftstate := w.Bytes()
	// rf.persister.Save(raftstate, nil)
}

// 取回持久化的raft数据
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// TODO (2C) Your code here .
	// Example:
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var CurrentTerm int
	var VotedFor int
	var Logs []LogEntry
	if err := d.Decode(&CurrentTerm); err != nil {
		Debug(dLog, "S%d cannot readPersist due to %v ", rf.me, err)
		return
	}
	if err := d.Decode(&VotedFor); err != nil {
		Debug(dLog, "S%d cannot readPersist due to %v ", rf.me, err)
		return
	}
	if err := d.Decode(&Logs); err != nil {
		Debug(dLog, "S%d cannot readPersist due to %v ", rf.me, err)
		return
	}
	Debug(dLog, "S%d readPersist %d %d %v", rf.me, CurrentTerm, VotedFor, Logs)
	rf.CurrentTerm, rf.VotedFor, rf.Log = CurrentTerm, VotedFor, Logs
}

// the service says it has created a snapshot that has
// all info up to and including index. this means the
// service no longer needs the log through (and including)
// that index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// TODO (2D) Your code here .

}

type RequestVoteArgs struct {
	Term         int //候选者的任期
	CandidateId  int //请求投票的候选者Id
	LastLogIndex int //候选者最后一条日志的下标
	LastLogTerm  int //候选者最后一条日志对应的任期
}

type RequestVoteReply struct {
	Term        int  //接收者当前的任期；如果候选者的任期小于接收者的任期，候选者需要更新自己的状态
	VoteGranted bool //true表示接收者向候选者投票；返回false如果接收者的任期大于候选者的任期
}

func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	defer rf.persist()
	if args.Term < rf.CurrentTerm || (args.Term == rf.CurrentTerm && rf.VotedFor != -1 && rf.VotedFor != args.CandidateId) {
		// 候选者的任期小于当前服务器的任期或者当前任期已经投过票了，因此不会再去投票了
		reply.Term, reply.VoteGranted = rf.CurrentTerm, false
		return
	}
	if args.Term > rf.CurrentTerm {
		// 当有一个更大的任期到来时，应当立刻转变当前的任期($5.1)
		rf.state = FOLLOWER
		rf.CurrentTerm, rf.VotedFor = args.Term, -1
	}
	if !rf.isLogUpToDate(args.LastLogTerm, args.LastLogIndex) {
		reply.Term, reply.VoteGranted = rf.CurrentTerm, false
		return
	}
	rf.VotedFor = args.CandidateId
	reply.Term, reply.VoteGranted = rf.CurrentTerm, true
	rf.electionTimer.Reset(randomizedElectionTimeout())
	Debug(dVote, "S%d Granting Vote to S%d at T%d", rf.me, args.CandidateId, args.Term)
}

// 检查请求的日志是否比当前的新,如果是最新的，返回true，否则返回false
func (rf *Raft) isLogUpToDate(lastLogTerm, lastLogIndex int) bool {
	curLastLogIndex := len(rf.Log) - 1             //日志记录的最后一条记录的下标
	curLastLogTerm := rf.Log[curLastLogIndex].Term //日志记录的最后一条记录的任期
	return curLastLogTerm < lastLogTerm || (lastLogTerm == curLastLogTerm && curLastLogIndex <= lastLogIndex)
}

// 对server发送RPC请求
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

// 添加条目的请求结构
type AppendEntriesArgs struct {
	Term         int        // leader的任期
	LeaderId     int        // leader的标识符
	PrevLogIndex int        // 紧接新日志条目之前的日志条目索引
	PrevLogTerm  int        // prevLogIndex下标对应的term
	Entries      []LogEntry // 日志条目存储(空为心跳;为了提高效率,可能发送多个)
	LeaderCommit int        // leader的commitIndex
}

// 创建携带日志的AppendEntries
func (rf *Raft) createAppendEntriesArgs(prevLogIndex int) AppendEntriesArgs {
	index := 0 //找到prevLogIndex对应日志中对应的下标
	for i := range rf.Log {
		if rf.Log[i].Index == prevLogIndex {
			index = i
		}
	}
	return AppendEntriesArgs{Term: rf.CurrentTerm,
		LeaderId:     rf.me,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  rf.Log[prevLogIndex].Term,
		Entries:      rf.Log[index+1 : len(rf.Log)],
		LeaderCommit: rf.commitIndex,
	}
}

// 添加条目的响应结构
type AppendEntriesReply struct {
	Term    int  //当前raft节点的任期，可用于leader更新
	Success bool //如果Follower包含匹配PrevLogIndex与PrevLogTerm值的条目，则为true
	XTerm   int  //与PrevLogIndex如果存在冲突，返回冲突的任期
	XIndex  int  //冲突所在的任期的第一个下标
}

// 日志是否匹配
func (rf *Raft) isLogMatched(logTerm, logIndex int) bool {
	return logIndex <= rf.getLastLog().Index && rf.Log[logIndex-rf.getFirstLog().Index].Term == logTerm
}

func (rf *Raft) advanceCommitIndexForFollower(currentTerm, leaderCommit int) {
	if rf.getLastLog().Term == currentTerm { //如果日志是最新的
		rf.commitIndex = minInt(leaderCommit, rf.getLastLog().Index)
		rf.applyCond.Signal()
	}
}

// 处理AppendEntries请求
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	defer rf.persist()
	if args.Term < rf.CurrentTerm {
		reply.Term, reply.Success = rf.CurrentTerm, false
		return
	}
	if args.Term > rf.CurrentTerm {
		rf.CurrentTerm, rf.VotedFor = args.Term, -1
	}
	rf.changeState(FOLLOWER)
	rf.electionTimer.Reset(randomizedElectionTimeout())
	if !rf.isLogMatched(args.PrevLogTerm, args.PrevLogIndex) { //日志不匹配
		reply.Term, reply.Success = rf.CurrentTerm, false
		lastIndex := rf.getLastLog().Index
		if args.PrevLogIndex > lastIndex {
			reply.XTerm, reply.XIndex = -1, lastIndex+1
		} else {
			reply.XTerm = rf.Log[args.PrevLogIndex].Term
			index := args.PrevLogIndex - 1
			for index >= 1 && rf.Log[index].Term == reply.XTerm {
				index--
			}
			reply.XIndex = index + 1
		}
		return
	}
	if len(args.Entries) != 0 {
		nextIndex := args.PrevLogIndex + 1 //从完全匹配的下一刻起更新日志
		i := 0
		for ; i < len(args.Entries) && nextIndex <= rf.getLastLog().Index; i, nextIndex = i+1, nextIndex+1 {
			if rf.Log[nextIndex].Term == args.Entries[i].Term {
				continue
			} else {
				rf.Log = rf.Log[:nextIndex] // TODO 3D
				break
			}
		}
		for ; i < len(args.Entries); i++ {
			rf.Log = append(rf.Log, args.Entries[i])
			Debug(dInfo, "S%d's lastLog %v at T%d", rf.me, rf.getLastLog(), rf.CurrentTerm)
			nextIndex++
		}
	}
	rf.advanceCommitIndexForFollower(args.Term, args.LeaderCommit)
	reply.Term, reply.Success = rf.CurrentTerm, true
}

// 向raft对等体发送AppendEntry
func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

// 向日志中添加新的条目
func (rf *Raft) appendNewLogEntry(command interface{}) LogEntry {
	newLogEntry := LogEntry{Term: rf.CurrentTerm, Command: command, Index: rf.getLastLog().Index + 1}
	rf.Log = append(rf.Log, newLogEntry)
	rf.matchIndex[rf.me] = newLogEntry.Index
	Debug(dLeader, "S%d append %v at T%d", rf.me, newLogEntry, rf.CurrentTerm)
	return newLogEntry
}

// 向raft执行命令
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.state != LEADER {
		return -1, -1, false
	}
	newLogEntry := rf.appendNewLogEntry(command)
	rf.broadcast(REPLICATE)
	return newLogEntry.Index, newLogEntry.Term, true
}

// 广播
func (rf *Raft) broadcast(broadcastType BroadcastType) {
	if rf.state != LEADER {
		return
	}
	switch broadcastType {
	case HEARTBEAT:
		Debug(dLeader, "S%d broadcast HeartBeat at T%d with commitIndex %d", rf.me, rf.CurrentTerm, rf.commitIndex)
		for peer := range rf.peers {
			if peer == rf.me {
				continue
			}
			go rf.heartBeat(peer)
		}
	case REPLICATE:
		Debug(dLeader, "S%d broadcast Log at T%d", rf.me, rf.CurrentTerm)
		for peer := range rf.peers {
			if peer == rf.me {
				continue
			}
			rf.replicatorCond[peer].Signal()
		}
	}
}

func (rf *Raft) changeState(state RaftState) {
	switch state {
	case LEADER:
		rf.state = LEADER
		for i := range rf.nextIndex {
			if i != rf.me {
				rf.nextIndex[i] = rf.getLastLog().Index + 1
				rf.matchIndex[i] = 0
			} else {
				rf.matchIndex[i] = rf.getLastLog().Index
			}
		}
	case CANDIDATE:
		rf.state = CANDIDATE
	case FOLLOWER:
		rf.state = FOLLOWER
	}
}

// 开始选举
func (rf *Raft) startElect() {
	rf.VotedFor = rf.me
	voteCnt := 1 //自己投自己一票
	rf.persist()
	lastLogEntry := rf.getLastLog() // 使用golang的闭包，所以不用go func的时候不用传输参数
	defer Debug(dVote, "S%d start Elect at T%d,lastLog is %v", rf.me, rf.CurrentTerm, lastLogEntry)
	for peer := range rf.peers {
		if peer == rf.me {
			continue
		}
		go func(peer int, term int) { // 向peer拉票
			args := &RequestVoteArgs{Term: term, CandidateId: rf.me, LastLogIndex: lastLogEntry.Index, LastLogTerm: lastLogEntry.Term}
			reply := &RequestVoteReply{}
			ok := false
			for !ok {
				ok = rf.sendRequestVote(peer, args, reply)
				if ok {
					rf.mu.Lock()
					defer rf.mu.Unlock()
					if reply.Term == rf.CurrentTerm && rf.state == CANDIDATE {
						if reply.VoteGranted {
							voteCnt++
							if voteCnt > len(rf.peers)/2 {
								//成功当选
								rf.changeState(LEADER)
								Debug(dVote, "S%d win elect at T%d and commitIndex:%d,lastLogIndex:%d",
									rf.me, rf.CurrentTerm, rf.commitIndex, rf.getLastLog().Index)
								rf.heartbeatTimer.Reset(0)
							}
						}
					} else if reply.Term > rf.CurrentTerm {
						//当前的任期不是最新的
						Debug(dVote, "S%d finds a raft peer S%d with T%d,so convert to follower", rf.me, peer, reply.Term)
						rf.changeState(FOLLOWER)
						rf.CurrentTerm, rf.VotedFor = reply.Term, -1
						rf.persist() //任期与投递的票修改了，因此要持久化
					}
				}
			}
		}(peer, rf.CurrentTerm)
	}
}

// 向peer发送心跳
func (rf *Raft) heartBeat(peer int) {
	rf.mu.RLock()
	if rf.state != LEADER { //不是Leader了
		rf.mu.RUnlock()
		return
	}
	prevLogIndex := rf.nextIndex[peer] - 1
	args := AppendEntriesArgs{Term: rf.CurrentTerm,
		LeaderId:     rf.me,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  rf.Log[prevLogIndex].Term,
		Entries:      nil,
		LeaderCommit: rf.commitIndex,
	}
	rf.mu.RUnlock()
	reply := AppendEntriesReply{}
	if rf.sendAppendEntries(peer, &args, &reply) {
		rf.mu.Lock()
		rf.handleHearBeat(&args, &reply)
		rf.mu.Unlock()
	}

}

// 处理心跳
func (rf *Raft) handleHearBeat(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	if args.Term == rf.CurrentTerm && reply.Term > rf.CurrentTerm {
		rf.changeState(FOLLOWER)
		rf.CurrentTerm, rf.VotedFor = reply.Term, -1
		rf.persist()
		return
	}
}

// 向peer发送一轮日志
func (rf *Raft) replicateOneRound(peer int) {
	rf.mu.RLock()
	if rf.state != LEADER { //不是Leader了
		rf.mu.RUnlock()
		return
	}
	prevLogIndex := rf.nextIndex[peer] - 1
	if prevLogIndex < rf.getFirstLog().Index {
		// For 3D
		return
	} else {
		args := rf.createAppendEntriesArgs(prevLogIndex)
		Debug(dLeader, "S%d send Log to S%d prevLogIndex:%d at T%d", rf.me, peer, prevLogIndex, rf.CurrentTerm)
		rf.mu.RUnlock()
		reply := AppendEntriesReply{}
		if rf.sendAppendEntries(peer, &args, &reply) {
			rf.mu.Lock()
			rf.handleReplicateOneRoundResponse(peer, &args, &reply)
			rf.mu.Unlock()
		}
	}
}

// 判断是否大多数节点收到了N
func (rf *Raft) hasBeenAppendedMajority(N int) bool {
	cnt := 0
	for _, v := range rf.matchIndex {
		if v >= N {
			cnt++
		}
	}
	return cnt > len(rf.peers)/2 && rf.Log[N].Term == rf.CurrentTerm
}

// 处理复制日志响应
func (rf *Raft) handleReplicateOneRoundResponse(peer int, args *AppendEntriesArgs, reply *AppendEntriesReply) {
	if reply.Term > args.Term { // Follower的任期大于发送时的任期，直接结束
		if reply.Term > rf.CurrentTerm { // Follower的任期大于发送方的任期，更新任期与状态
			rf.CurrentTerm, rf.state, rf.VotedFor = reply.Term, FOLLOWER, -1
		}
		rf.persist()
		return
	}
	if reply.Success { //发出去的日志已经被接收了
		rf.matchIndex[peer] = args.PrevLogIndex + len(args.Entries)
		rf.nextIndex[peer] = args.PrevLogIndex + len(args.Entries) + 1
		left, right := rf.commitIndex+1, rf.getLastLog().Index //使用二分进行查找
		for left <= right {
			mid := (left + right) / 2
			if rf.hasBeenAppendedMajority(mid) {
				//感觉这个单调性不是很明确，所以我摆烂了，直接在此处更新
				rf.commitIndex = maxInt(rf.commitIndex, mid)
				rf.applyCond.Signal()
				left++
			} else {
				right--
			}
		}
	} else { //更新nextIndex
		Debug(dLeader, "S%d know Confilct with S%d", rf.me, peer)
		i := len(rf.Log) - 1
		isXTermExist := false // 检查在日志中是否存在XTerm
		for ; i >= 0; i-- {
			if rf.Log[i].Term == reply.XTerm {
				isXTermExist = true
				break
			}
		}
		if !isXTermExist { //如果日志中不存在XTerm
			rf.nextIndex[peer] = reply.XIndex
		} else { //如果当前日志存在XTerm
			rf.nextIndex[peer] = i
		}
	}
}

// 计时器
func (rf *Raft) ticker() {
	// 如果raft节点还在运行
	for !rf.killed() {
		select {
		case <-rf.electionTimer.C: //选举超时
			rf.mu.Lock()
			if rf.state != LEADER {
				rf.changeState(CANDIDATE)
				rf.CurrentTerm++ //任期自增
				rf.startElect()
			}
			rf.electionTimer.Reset(randomizedElectionTimeout())
			rf.mu.Unlock()
		case <-rf.heartbeatTimer.C: //心跳超时
			rf.mu.Lock()
			if rf.state == LEADER {
				rf.broadcast(HEARTBEAT)
				rf.heartbeatTimer.Reset(HEARTBEATETIME) //重置心跳
			}
			rf.mu.Unlock()
		}
	}
}

// 判断是否需要复制日志
func (rf *Raft) needReplicatingLog(peer int) bool {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.state == LEADER && rf.matchIndex[peer] < rf.getLastLog().Index
}

// 向peer发送日志条目
func (rf *Raft) replicator(peer int) {
	rf.replicatorCond[peer].L.Lock()
	defer rf.replicatorCond[peer].L.Unlock()
	for !rf.killed() {
		for !rf.needReplicatingLog(peer) {
			rf.replicatorCond[peer].Wait()
		}
		rf.replicateOneRound(peer)
	}
}

// 判断是否需要应用日志
func (rf *Raft) needApplyingLog() bool {
	return rf.commitIndex > rf.lastApplied
}

// 应用日志条目
func (rf *Raft) applyLogs() {
	for i := rf.lastApplied + 1; i <= rf.commitIndex; i++ {
		rf.applyCh <- ApplyMsg{
			CommandValid: true,
			Command:      rf.Log[i].Command,
			CommandIndex: rf.Log[i].Index,
		}
		Debug(dApply, "S%d apply %v at T%d",
			rf.me, rf.Log[i], rf.CurrentTerm)
		rf.lastApplied++
	}
}

// 应用日志条目协程
func (rf *Raft) applyTicker() {
	rf.applyCond.L.Lock()
	defer rf.applyCond.L.Unlock()
	for !rf.killed() {
		for !rf.needApplyingLog() {
			rf.applyCond.Wait()
		}
		rf.applyLogs()
	}
}

// 终止raft节点
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
}

// 检查raft节点是否终止
func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}
