package raft

import (
	"bytes"
	"sort"
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
	// 论文不要求持久化的变量，考虑到snapshot的影响，我把它持久化了
	CommitIndex int //已知的已提交的最高日志条目的索引（初始化为 0，单调递增）
	LastApplied int //应用于状态机的最高日志条目的索引（初始化为 0，单调递增）
	//非持久化的变量
	state     RaftState     //表示当前节点状态
	applyCh   chan ApplyMsg //当日志条目提交时，向管道写入ApplyMsg
	applyCond *sync.Cond    //用于应用日志
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
		CommitIndex: 0,
		LastApplied: 0,
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
	go rf.applier()
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

// 创建raftstate的持久化
func (rf *Raft) encodeRaftState() []byte {
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	// Encode与decode的顺序要一一对应，否则无法正确decode
	e.Encode(rf.CurrentTerm)
	e.Encode(rf.VotedFor)
	e.Encode(rf.Log)
	return w.Bytes()
}

// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
// before you've implemented snapshots, you should pass nil as the
// second argument to persister.Save().
// after you've implemented snapshots, pass the current snapshot
// (or nil if there's not yet a snapshot).
func (rf *Raft) persist() {
	defer DPrintf(dLog, "S%d persist at {T:%d,VFOR:S%d,Log:%v,CI:%d,LI:%d},", rf.me, rf.CurrentTerm, rf.VotedFor, rf.Log, rf.CommitIndex, rf.LastApplied)
	rf.persister.Save(rf.encodeRaftState(), rf.persister.ReadSnapshot())
}

// 取回持久化的raft数据
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	var CurrentTerm int
	var VotedFor int
	var Logs []LogEntry
	if d.Decode(&CurrentTerm) != nil || d.Decode(&VotedFor) != nil || d.Decode(&Logs) != nil {
		DPrintf(dError, "S%d can not readPersist", rf.me)
	}
	rf.CurrentTerm, rf.VotedFor, rf.Log = CurrentTerm, VotedFor, Logs
	rf.CommitIndex = rf.getFirstLog().Index
	DPrintf(dLog, "S%d readPersist {T%d,S%d,%v,CI:%d,LI:%d}", rf.me, CurrentTerm, VotedFor, Logs, rf.CommitIndex, rf.LastApplied)
	if snapshot := rf.persister.ReadSnapshot(); len(snapshot) > 1 {
		go func(firstIndex, firstTerm int) {
			rf.applyCh <- ApplyMsg{
				SnapshotValid: true,
				Snapshot:      snapshot,
				SnapshotTerm:  firstTerm,
				SnapshotIndex: firstIndex,
			}
			rf.mu.Lock()
			rf.LastApplied = maxInt(rf.LastApplied, firstIndex)
			rf.mu.Unlock()
		}(rf.getFirstLog().Index, rf.getFirstLog().Term)
	}
}

// 快照函数，snapshot由状态机提供
func (rf *Raft) Snapshot(lastIncludedIndex int, snapshot []byte) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	snapshotIndex := rf.getFirstLog().Index
	if lastIncludedIndex <= snapshotIndex {
		DPrintf(dSnap, "S%d current snapshotIndex %d > lastIncludedIndex %d", rf.me, snapshotIndex, lastIncludedIndex)
		return
	}
	// 使用lastIncludedIndex-firstIndex而非lastIncludedIndex-firstIndex保证len(rf.log)>=1
	rf.Log = shrinkEntriesArray(rf.Log[lastIncludedIndex-snapshotIndex:])
	DPrintf(dSnap, "S%d'state %v after snapshot", rf.me, rf.Log)
	rf.persister.Save(rf.encodeRaftState(), snapshot)
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
	if args.Term < rf.CurrentTerm || (args.Term == rf.CurrentTerm && rf.VotedFor != -1 && rf.VotedFor != args.CandidateId) {
		// 候选者的任期小于当前服务器的任期或者当前任期已经投过票了，因此不会再去投票了
		reply.Term, reply.VoteGranted = 0, false
		return
	}
	defer rf.persist()
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
	DPrintf(dVote, "S%d Granting Vote to S%d at T%d", rf.me, args.CandidateId, args.Term)
}

// 检查请求的日志是否比当前的新,如果是最新的，返回true，否则返回false
func (rf *Raft) isLogUpToDate(requestLastLogTerm, requestLastLogIndex int) bool {
	curLastLogIndex := rf.getLastLog().Index //当前日志记录的最后一条记录的下标
	curLastLogTerm := rf.getLastLog().Term   //当前日志记录的最后一条记录的任期
	return curLastLogTerm < requestLastLogTerm ||
		(requestLastLogTerm == curLastLogTerm && curLastLogIndex <= requestLastLogIndex)
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

// 创建AppendEntries，heartbeat表示是否为心跳创建
func (rf *Raft) createAppendEntriesArgs(prevLogIndex int) *AppendEntriesArgs {
	index := 0 //找到prevLogIndex对应日志中对应的下标
	for i := range rf.Log {
		if rf.Log[i].Index == prevLogIndex {
			index = i
		}
	}
	return &AppendEntriesArgs{Term: rf.CurrentTerm,
		LeaderId:     rf.me,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  rf.Log[index].Term,
		Entries:      rf.Log[index+1:],
		LeaderCommit: rf.CommitIndex,
	}
}

// 添加条目的响应结构
type AppendEntriesReply struct {
	Term    int  //当前raft节点的任期，可用于leader更新
	Success bool //如果Follower包含匹配PrevLogIndex与PrevLogTerm值的条目，则为true
	XTerm   int  //与PrevLogIndex如果存在冲突，返回冲突的任期
	XIndex  int  //冲突所在的任期的第一个下标
	XLen    int  //如果存在冲突，返回Follower当前的日志长度
}

// 日志是否匹配
func (rf *Raft) isLogMatched(requestPrevLogTerm, requestPrevLogIndex int) bool {
	firstIndex, lastIndex := rf.getFirstLog().Index, rf.getLastLog().Index
	if requestPrevLogIndex <= firstIndex { // 这里如果取小于号会出错
		return true
	}
	// if requestPrevLogIndex == firstIndex {

	// }
	return requestPrevLogIndex <= lastIndex && rf.Log[requestPrevLogIndex-firstIndex].Term == requestPrevLogTerm
}

func (rf *Raft) advanceCommitIndexForFollower(currentTerm, leaderCommit int) {
	if rf.getLastLog().Term == currentTerm { //如果日志是最新的
		rf.CommitIndex = minInt(leaderCommit, rf.getLastLog().Index)
		rf.applyCond.Signal()
	}
}

// 处理AppendEntries请求
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if args.Term < rf.CurrentTerm {
		reply.Term, reply.Success = rf.CurrentTerm, false
		return
	}
	defer rf.persist()
	if args.Term > rf.CurrentTerm {
		rf.CurrentTerm, rf.VotedFor = args.Term, -1
	}
	rf.changeState(FOLLOWER)
	rf.electionTimer.Reset(randomizedElectionTimeout())
	if !rf.isLogMatched(args.PrevLogTerm, args.PrevLogIndex) { //日志不匹配
		reply.Term, reply.Success = rf.CurrentTerm, false
		firstIndex, lastIndex := rf.getFirstLog().Index, rf.getLastLog().Index
		if args.PrevLogIndex > lastIndex { //当前的日志太短了
			reply.XTerm, reply.XIndex, reply.XLen = -1, lastIndex+1, firstIndex+1
		} else {
			reply.XTerm = rf.Log[args.PrevLogIndex-firstIndex].Term
			index := args.PrevLogIndex - 1
			for index >= firstIndex && rf.Log[index-firstIndex].Term == reply.XTerm {
				index--
			}
			reply.XIndex = index + 1 // 返回的XIndex是当前日志中第一个与Xterm匹配的下标
		}
		return
	}
	DPrintf(dTerm, "S%d receive %v from S%d at T%d", rf.me, args.Entries, args.LeaderId, rf.CurrentTerm)
	firstIndex := rf.getFirstLog().Index
	for index, entry := range args.Entries {
		if entry.Index >= firstIndex {
			if entry.Index-firstIndex >= len(rf.Log) || rf.Log[entry.Index-firstIndex].Term != entry.Term {
				rf.Log = shrinkEntriesArray(append(rf.Log[:entry.Index-firstIndex], args.Entries[index:]...))
				break
			}
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
	defer rf.persist()
	newLogEntry := LogEntry{Term: rf.CurrentTerm, Command: command, Index: rf.getLastLog().Index + 1}
	rf.Log = append(rf.Log, newLogEntry)
	rf.matchIndex[rf.me] = newLogEntry.Index
	DPrintf(dLeader, "S%d append %v at T%d", rf.me, newLogEntry, rf.CurrentTerm)
	return newLogEntry
}

// 由Leader调用，向Follower发送snapshot chunk。Leader总是按顺序发送snapshot chunk。
type InstallSnapshotArgs struct {
	Term              int // leader的任期
	LeaderId          int // leader的标识符
	LastIncludedIndex int // 快照会替换包括该索引在内的所有条目
	LastIncludedTerm  int // LastIncludedIndex对应的term
	// Offset            int    // 字节偏移：Chunk在snapshot文件中的位置
	Data []byte // 快照块的原始字节，从0开始
	// Done bool   // true如果这是最后一个Chunk
}

func (rf *Raft) createInstallSnapshotArgs(int) InstallSnapshotArgs {
	return InstallSnapshotArgs{
		Term:              rf.CurrentTerm,
		LeaderId:          rf.me,
		LastIncludedIndex: rf.getFirstLog().Index,
		LastIncludedTerm:  rf.getFirstLog().Term,
		Data:              rf.persister.ReadSnapshot(),
	}
}

type InstallSnapshotReply struct {
	Term int //当前raft节点的任期，可用于Leader更新
}

// Follower节点安装快照
func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	reply.Term = rf.CurrentTerm
	if args.Term < rf.CurrentTerm {
		return
	}
	if args.Term > rf.CurrentTerm {
		rf.CurrentTerm, rf.VotedFor = args.Term, -1
		rf.persist()
	}
	rf.state = FOLLOWER
	rf.electionTimer.Reset(randomizedElectionTimeout())
	// 如果当前commitIndex大于等于LastIncludedIndex，则不做处理
	if args.LastIncludedIndex <= rf.CommitIndex {
		return
	}

	if args.LastIncludedIndex > rf.getLastLog().Index {
		rf.Log = []LogEntry{{args.LastIncludedTerm, nil, args.LastIncludedIndex}}
	} else {
		rf.Log = shrinkEntriesArray(rf.Log[args.LastIncludedIndex-rf.getFirstLog().Index:])
	}
	// 更新lastApplied与commitIndex
	rf.CommitIndex = args.LastIncludedIndex
	rf.persister.Save(rf.encodeRaftState(), args.Data)
	// 在无锁环境下提交快照
	go func(snapshot []byte, lastIncludedIndex, lastIncludedTerm int) {
		rf.applyCh <- ApplyMsg{
			SnapshotValid: true,
			Snapshot:      snapshot,
			SnapshotTerm:  lastIncludedTerm,
			SnapshotIndex: lastIncludedIndex,
		}
		rf.mu.Lock()
		rf.LastApplied = maxInt(rf.LastApplied, lastIncludedIndex)
		DPrintf(dCommit, "S%d update LI:%d because of snapshot", rf.me, rf.LastApplied)
		rf.mu.Unlock()
	}(args.Data, args.LastIncludedIndex, args.LastIncludedTerm)
}

// 向raft对等体发送AppendEntry
func (rf *Raft) sendInstallSnapshot(server int, args *InstallSnapshotArgs, reply *InstallSnapshotReply) bool {
	ok := rf.peers[server].Call("Raft.InstallSnapshot", args, reply)
	return ok
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
		// DPrintf(dLeader, "S%d broadcast HeartBeat at T%d with commitIndex %d", rf.me, rf.CurrentTerm, rf.commitIndex)
		for peer := range rf.peers {
			if peer == rf.me {
				continue
			}
			// go rf.heartBeat(peer)
			go rf.replicateOneRound(peer)
		}
	case REPLICATE:
		// DPrintf(dLeader, "S%d broadcast Log at T%d", rf.me, rf.CurrentTerm)
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
	rf.CurrentTerm++                //任期自增
	voteCnt := 1                    //自己投自己一票
	lastLogEntry := rf.getLastLog() // 使用golang的闭包，所以不用go func的时候不用传输参数
	DPrintf(dVote, "S%d start Elect at T%d,lastLog is %v",
		rf.me, rf.CurrentTerm, lastLogEntry)
	rf.persist()
	for peer := range rf.peers {
		if peer == rf.me {
			continue
		}
		go func(peer int, term int) { // 向peer拉票
			args := &RequestVoteArgs{Term: term,
				CandidateId:  rf.me,
				LastLogIndex: lastLogEntry.Index,
				LastLogTerm:  lastLogEntry.Term}
			reply := &RequestVoteReply{}
			DPrintf(dVote, "S%d -> S%d RV ", rf.me, peer)
			if rf.sendRequestVote(peer, args, reply) {
				rf.mu.Lock()
				defer rf.mu.Unlock()
				if reply.Term > rf.CurrentTerm { //当前的任期不是最新的
					DPrintf(dVote, "S%d -> T%d follower  ", rf.me, reply.Term)
					rf.changeState(FOLLOWER)
					rf.CurrentTerm, rf.VotedFor = reply.Term, -1
					rf.persist() //任期与投递的票修改了，因此要持久化
					return
				}
				if reply.Term == rf.CurrentTerm {
					if reply.VoteGranted {
						voteCnt++
						if voteCnt > len(rf.peers)/2 && rf.state != LEADER {
							//成功当选
							rf.changeState(LEADER)
							DPrintf(dVote, "S%d win elect at T%d and {CI:%d,LLI:%d,LLT:%d}",
								rf.me, rf.CurrentTerm, rf.CommitIndex,
								rf.getLastLog().Index, rf.getLastLog().Term)
							rf.broadcast(HEARTBEAT)
							rf.heartbeatTimer.Reset(HEARTBEATETIME) //重置心跳
						}
					}
				}
			}
		}(peer, rf.CurrentTerm)
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
	if prevLogIndex < rf.getFirstLog().Index { // 要发送的日志下标不在当前日志中 3D
		args := rf.createInstallSnapshotArgs(prevLogIndex)
		rf.mu.RUnlock()
		reply := &InstallSnapshotReply{}
		if rf.sendInstallSnapshot(peer, &args, reply) {
			rf.mu.Lock()
			rf.handleInstallSnapshotResponse(peer, &args, reply)
			rf.mu.Unlock()
		}
	} else {
		args := rf.createAppendEntriesArgs(prevLogIndex)
		DPrintf(dLeader, "S%d -> S%d AE sending {PLI:%d,PLT:%d} at T%d", rf.me, peer, prevLogIndex, args.PrevLogTerm, rf.CurrentTerm)
		rf.mu.RUnlock()
		reply := &AppendEntriesReply{}
		if rf.sendAppendEntries(peer, args, reply) {
			rf.mu.Lock()
			rf.handleReplicateOneRoundResponse(peer, args, reply)
			rf.mu.Unlock()
		}
	}
}

func (rf *Raft) handleInstallSnapshotResponse(peer int, args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	if args.Term != rf.CurrentTerm || rf.state != LEADER { // 移除过时的响应
		return
	}
	if reply.Term > rf.CurrentTerm { // Follower的任期大于发送时的任期，直接结束
		rf.changeState(FOLLOWER)
		rf.CurrentTerm, rf.VotedFor = reply.Term, -1
		rf.persist()
		return
	}
	// 更新matchIndex与nextIndex
	rf.matchIndex[peer] = maxInt(rf.matchIndex[peer], args.LastIncludedIndex)
	rf.nextIndex[peer] = maxInt(rf.nextIndex[peer], args.LastIncludedIndex+1)
}

// 判断是否大多数节点收到了下标N所对应的日志条目
func (rf *Raft) hasBeenAppendedMajority(N int) bool {
	firstIndex := rf.getFirstLog().Index
	if N <= firstIndex {
		DPrintf(dLeader, "S%d receive N:%d < firstIndex:%d,why?", rf.me, N, firstIndex)
		return true
	}
	cnt := 0
	for _, v := range rf.matchIndex {
		if v >= N {
			cnt++
		}
	}
	return cnt > len(rf.peers)/2 && rf.Log[N-firstIndex].Term == rf.CurrentTerm
}

// 处理复制日志响应
func (rf *Raft) handleReplicateOneRoundResponse(peer int, args *AppendEntriesArgs, reply *AppendEntriesReply) {
	if args.Term != rf.CurrentTerm || rf.state != LEADER { // 移除过时的响应
		return
	}
	if reply.Term > rf.CurrentTerm { // Follower的任期大于发送时的任期，直接结束
		rf.changeState(FOLLOWER)
		rf.CurrentTerm, rf.VotedFor = reply.Term, -1
		rf.persist()
		return
	}
	if reply.Success { //发出去的日志已经被接收了
		rf.matchIndex[peer] = maxInt(rf.matchIndex[peer], args.PrevLogIndex+len(args.Entries))
		rf.nextIndex[peer] = maxInt(rf.nextIndex[peer], args.PrevLogIndex+len(args.Entries)+1)
		// 无需区分heartbeat和appendEntries，因为提交本地日志可以由rf.Log[N-firstIndex].Term == rf.CurrentTerm保证
		left, right := rf.CommitIndex+1, rf.getLastLog().Index
		for N := right; N >= left; N-- {
			if rf.hasBeenAppendedMajority(N) {
				rf.CommitIndex = maxInt(rf.CommitIndex, N)
				rf.applyCond.Signal()
				break //成功了就可以直接退出了
			}
		}
	} else { //更新nextIndex
		if reply.XTerm == -1 { //Follower的日志太短了
			rf.nextIndex[peer] = reply.XLen
			DPrintf(dLeader, "S%d conflict with S%d,nextIndex: %d", rf.me, peer, rf.nextIndex[peer])
			return
		}
		i := sort.Search(len(rf.Log), func(i int) bool { // 二分查找优化
			return rf.Log[i].Term > reply.XTerm
		}) // i是日志中，第一个下标的Term大于XTerm的下标
		if i > 0 && rf.Log[i-1].Term == reply.XTerm { //如果当前日志存在XTerm
			rf.nextIndex[peer] = rf.Log[i].Index //设nextIndex为当前XTerm的最后一个日志
		} else { //如果日志中不存在XTerm
			rf.nextIndex[peer] = reply.XIndex
		}
		DPrintf(dLeader, "S%d conflict with S%d,nextIndex: %d", rf.me, peer, rf.nextIndex[peer])
	}
}

// 计时器
func (rf *Raft) ticker() {
	// 如果raft节点还在运行，当计时器触发的时候，rf可能已经死了
	for !rf.killed() {
		select {
		case <-rf.electionTimer.C: //选举超时
			rf.mu.Lock()
			if rf.state != LEADER {
				rf.changeState(CANDIDATE)
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
	return rf.CommitIndex > rf.LastApplied && rf.LastApplied >= rf.getFirstLog().Index
}

// 异步应用日志条目协程
func (rf *Raft) applier() {
	for !rf.killed() {
		rf.applyCond.L.Lock()
		for !rf.needApplyingLog() {
			rf.applyCond.Wait()
		}
		firstIndex, commitIndex, lastApplied := rf.getFirstLog().Index, rf.CommitIndex, rf.LastApplied
		toBeApplyLogEntries := rf.Log[lastApplied+1-firstIndex : commitIndex+1-firstIndex]
		// txy的完全copy效率太低了，这里可以直接零拷贝，因为这部分是commit的
		// copy(toBeApplyLogEntries, rf.Log[lastApplied+1-firstIndex:commitIndex+1-firstIndex])
		rf.applyCond.L.Unlock() //释放锁，因为Push ApplyCh的时候不能持有锁
		for _, entry := range toBeApplyLogEntries {
			rf.applyCh <- ApplyMsg{
				CommandValid: true,
				Command:      entry.Command,
				CommandIndex: entry.Index,
			}
		}
		rf.applyCond.L.Lock()
		rf.LastApplied = maxInt(rf.LastApplied, commitIndex)
		DPrintf(dCommit, "S%d update LI:%d", rf.me, rf.LastApplied)
		rf.applyCond.L.Unlock()
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
