package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	//	"bytes"

	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	//	"6.5840/labgob"
	"6.5840/labrpc"
)

const HEARTBEATETIME = time.Duration(100) * time.Millisecond //心跳发送时间间隔

// 随机的选举时间
func randomizedElectionTimeout() time.Duration {
	ms := 150 + (rand.Int63() % 600)
	return time.Duration(ms) * time.Millisecond
}

// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in part 2D you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh, but set CommandValid to false for these
// other uses.
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

// Raft节点的状态类型，分为Leader,Candidate,Follower
type RaftState int

const (
	LEADER RaftState = iota
	CANDIDATE
	FOLLOWER
)

// 日志条目
type LogEntry struct {
	Term    int
	Command interface{}
}

// 实现单个Raft节点的go结构体
//
// A Go object implementing a single Raft peer.
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	// TODO (2A, 2B, 2C) Your data here .
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.
	//需要持久化的变量
	currentTerm int        //服务器已经见识过的任期（在启动时初始化为0，单调递增）
	votedFor    int        //当前任期，本服务器投票的候选人id，（如果没有则为空，实际实现中如果没有我设为-1）
	logs        []LogEntry //日志条目；每个条目包含命令和从领导者收到条目的任期，索引从1开始

	//非持久化的变量
	state       RaftState     //表示当前节点状态
	commitIndex int           //已知的已提交的最高日志条目的索引（初始化为 0，单调递增）
	lastApplied int           //应用于状态机的最高日志条目的索引（初始化为 0，单调递增）
	applyCh     chan ApplyMsg //当日志条目提交时，向管道写入ApplyMsg
	//非持久化的Leader使用的变量

	nextIndex  []int //对于每个服务器，要发送到该服务器的下一条日志条目的索引（初始化为领导者的最后一条日志索引 + 1）
	matchIndex []int //在每个服务器上已知已经复制的最高日志条目的索引（初始化为 0，单调递增）

	electionTimer  *time.Timer
	heartbeatTimer *time.Timer
}

// return currentTerm and whether this server
// believes it is the leader.
// 这个函数里面已经使用了mu锁了，不要重复加锁
func (rf *Raft) GetState() (int, bool) {

	var term int
	var isLeader bool
	// TODO (2A) Your code here .
	rf.mu.Lock()
	term = rf.currentTerm
	isLeader = (rf.state == LEADER)
	rf.mu.Unlock()
	return term, isLeader
}

// 返回日志的最后一项的下标与最后一项
func (rf *Raft) getLastLog() (int, LogEntry) {
	lastIndex := len(rf.logs) - 1
	return lastIndex, rf.logs[lastIndex]
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
	// w := new(bytes.Buffer)
	// e := labgob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// raftstate := w.Bytes()
	// rf.persister.Save(raftstate, nil)
}

// restore previously persisted state.
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// TODO (2C) Your code here .
	// Example:
	// r := bytes.NewBuffer(data)
	// d := labgob.NewDecoder(r)
	// var xxx
	// var yyy
	// if d.Decode(&xxx) != nil ||
	//    d.Decode(&yyy) != nil {
	//   error...
	// } else {
	//   rf.xxx = xxx
	//   rf.yyy = yyy
	// }
}

// the service says it has created a snapshot that has
// all info up to and including index. this means the
// service no longer needs the log through (and including)
// that index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// TODO (2D) Your code here .

}

// example RequestVote RPC arguments structure.
// field names must start with capital letters!
type RequestVoteArgs struct {
	// TODO (2A, 2B) Your data here .
	Term         int //候选者的任期
	CandidateId  int //请求投票的候选者Id
	LastLogIndex int //候选者最后一条日志的下标
	LastLogTerm  int //候选者最后一条日志对应的任期
}

// example RequestVote RPC reply structure.
// field names must start with capital letters!
type RequestVoteReply struct {
	// TODO (2A) Your data here .
	Term        int  //接收者当前的任期；如果候选者的任期小于接收者的任期，候选者需要更新自己的状态
	VoteGranted bool //true表示接收者向候选者投票；返回false如果接收者的任期大于候选者的任期
}

// example RequestVote RPC handler.
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// 这里我选择上锁的原因是，
	// 如果有多个投票请求同时到达,就有可能造成投了多张票
	rf.mu.Lock()
	defer rf.mu.Unlock()
	// TODO (2A, 2B) Your code here .

	if args.Term < rf.currentTerm || (args.Term == rf.currentTerm && rf.votedFor != -1 && rf.votedFor != args.CandidateId) {
		// 候选者的任期小于当前服务器的任期或者当前任期已经投过票了，因此不会再去投票了
		reply.Term, reply.VoteGranted = rf.currentTerm, false
		return
	}
	if args.Term > rf.currentTerm {
		// 当有一个更大的任期到来时，应当立刻转变当前的任期
		rf.state = FOLLOWER
		rf.currentTerm, rf.votedFor = args.Term, -1
	}
	// 获取当前raft节点的log的日志最后一条的任期与下标
	if !rf.isLogUpToDate(args.LastLogTerm, args.LastLogIndex) {
		// 候选者的日志条目的任期没有当前条目的任期大，因此拒绝
		// 候选者的日志条目的任期与当前一致，但是长度没有当前的长，因此拒绝
		reply.Term, reply.VoteGranted = rf.currentTerm, false
		return
	}
	rf.votedFor = args.CandidateId
	reply.Term, reply.VoteGranted = rf.currentTerm, true
	rf.electionTimer.Reset(randomizedElectionTimeout())
	Debug(dVote, "S%d Granting Vote to S%d at T%d", rf.me, args.CandidateId, args.Term)
}

// 检查请求的日志是否比当前的新,如果是最新的，返回true，否则返回false
func (rf *Raft) isLogUpToDate(lastLogTerm, lastLogIndex int) bool {
	curLastLogIndex := len(rf.logs) - 1             //日志记录的最后一条记录的下标
	curLastLogTerm := rf.logs[curLastLogIndex].Term //日志记录的最后一条记录的任期
	return curLastLogTerm < lastLogTerm || (lastLogTerm == curLastLogTerm && curLastLogIndex <= lastLogIndex)
}

// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

type AppendEntriesArgs struct {
	Term         int        // leader的任期
	LeaderId     int        // leader的标识符
	PrevLogIndex int        // 紧接新日志条目之前的日志条目索引
	PrevLogTerm  int        // prevLogIndex下标对应的term
	Entries      []LogEntry // 日志条目存储(空为心跳;为了提高效率,可能发送多个)
	LeaderCommit int        // leader的commitIndex
}

type AppendEntriesReply struct {
	Term                   int  //当前raft节点的任期，可用于leader更新
	Success                bool //如果follower包含匹配PrevLogIndex与PrevLogTerm值的条目，则为true
	ConflictTerm           int  //与PrevLogIndex如果存在冲突，返回冲突的任期
	ConflictTermFirstIndex int  //冲突所在的任期的第一个下标
}

// 日志是否匹配
func (rf *Raft) isLogMatched(logTerm, logIndex int) bool {
	return logIndex < len(rf.logs) && rf.logs[logIndex].Term == logTerm
}

// 应用日志条目
func (rf *Raft) applyLogs(matchIndex int, leaderCommit int) {
	rf.commitIndex = minInt(leaderCommit, len(rf.logs)-1)
	for i := rf.lastApplied + 1; i <= rf.commitIndex && i <= matchIndex; i++ {
		rf.applyCh <- ApplyMsg{
			CommandValid: true,
			Command:      rf.logs[i].Command,
			CommandIndex: i,
		}
		Debug(dApply, "S%d apply {Command:%v,Term:%d,CommandIndex:%d} at T%d", rf.me, rf.logs[i].Command, rf.logs[i].Term, i, rf.currentTerm)
		rf.lastApplied++
	}
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	// TODO A,B
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if args.Term < rf.currentTerm {
		reply.Term, reply.Success = rf.currentTerm, false
		return
	}
	if args.Term > rf.currentTerm {
		rf.currentTerm, rf.votedFor = args.Term, -1
	}
	rf.state = FOLLOWER //状态为Follower
	rf.electionTimer.Reset(randomizedElectionTimeout())
	if !rf.isLogMatched(args.PrevLogTerm, args.PrevLogIndex) { //日志不匹配
		reply.Term, reply.Success = rf.currentTerm, false
		lastIndex, _ := rf.getLastLog()
		if args.PrevLogIndex > lastIndex {
			reply.ConflictTerm, reply.ConflictTermFirstIndex = -1, lastIndex+1
		} else {
			reply.ConflictTerm = rf.logs[args.PrevLogIndex].Term
			index := args.PrevLogIndex - 1
			for index >= 1 && rf.logs[index].Term == reply.ConflictTerm {
				index--
			}
			reply.ConflictTermFirstIndex = index + 1
		}
		return
	}
	matchIndex := args.PrevLogIndex //与当前Leader完全匹配的下标
	if len(args.Entries) != 0 {
		nextIndex := args.PrevLogIndex + 1 //从完全匹配的下一刻起更新日志
		prevLogLength := len(rf.logs)      //当前的日志长度
		Debug(dFollower, "S%d receive S%d's logs[%d:%d] ", rf.me, args.LeaderId, nextIndex, nextIndex+len(args.Entries))
		for _, logEntry := range args.Entries {
			if nextIndex < prevLogLength { // 日志覆盖
				rf.logs[nextIndex] = logEntry
				nextIndex++
			} else { // 日志新增
				rf.logs = append(rf.logs, logEntry)
			}
		}
		matchIndex = len(rf.logs) - 1
	}
	rf.applyLogs(matchIndex, args.LeaderCommit)
	reply.Term, reply.Success = rf.currentTerm, true
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := true

	// TODO (2B) Your code here .
	rf.mu.Lock()
	defer rf.mu.Unlock()
	term = rf.currentTerm
	isLeader = (rf.state == LEADER)
	if isLeader {
		index = rf.broadcastLog(LogEntry{Term: term, Command: command})
	}
	return index, term, isLeader
}

// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// TODO Your code here, if desired.

}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

// 广播心跳
func (rf *Raft) broadcastHeartBeat() {
	if rf.state != LEADER {
		return
	}
	Debug(dLeader, "S%d broadcast heartbeat at T%d", rf.me, rf.currentTerm)
	leaderCommit := rf.commitIndex //当前已经提交的下标
	for peer := range rf.peers {
		if peer == rf.me {
			continue
		}
		prevLogIndex := rf.nextIndex[peer] - 1
		prevLogTerm := rf.logs[prevLogIndex].Term
		go func(peer int, currentTerm int) {
			// 心跳的话，发送的Entries条目为空
			args := AppendEntriesArgs{Term: currentTerm,
				LeaderId:     rf.me,
				PrevLogIndex: prevLogIndex,
				PrevLogTerm:  prevLogTerm,
				Entries:      nil,
				LeaderCommit: leaderCommit}
			reply := AppendEntriesReply{}
			ok := rf.sendAppendEntries(peer, &args, &reply)
			if ok {
				rf.mu.Lock()
				defer rf.mu.Unlock()
				if currentTerm == rf.currentTerm && reply.Term > rf.currentTerm {
					rf.currentTerm, rf.state, rf.votedFor = reply.Term, FOLLOWER, -1
				}
			}
		}(peer, rf.currentTerm)
	}
}

// 选举计时器
func (rf *Raft) ticker() {
	// 如果raft节点还在运行

	for !rf.killed() {
		// TODO (2A) Your code here
		// Check if a leader election should be started.
		select {
		case <-rf.electionTimer.C: //选举超时
			rf.mu.Lock()
			if rf.state != LEADER {
				rf.state = CANDIDATE
				rf.currentTerm++ //任期自增
				Debug(dVote, "S%d start Elect ", rf.me)
				rf.startElect()
			}
			rf.electionTimer.Reset(randomizedElectionTimeout())
			rf.mu.Unlock()
		case <-rf.heartbeatTimer.C: //心跳超时
			rf.mu.Lock()
			if rf.state == LEADER {
				rf.broadcastHeartBeat()
				rf.heartbeatTimer.Reset(HEARTBEATETIME)
			}
			rf.mu.Unlock()
		}
	}
}

// 开始选举
func (rf *Raft) startElect() {
	rf.votedFor = rf.me
	voteCnt := 1 //自己投自己一票
	// 使用golang的闭包，所以不用go func的时候不用传输参数
	lastLogTerm := rf.logs[len(rf.logs)-1].Term //日志记录的最后一条记录的任期
	lastLogIndex := len(rf.logs) - 1            //日志记录的最后一条记录的下标
	for peer := range rf.peers {
		if peer != rf.me {
			go func(peer int, term int) {
				//向其他人拉票
				args := &RequestVoteArgs{Term: term, CandidateId: rf.me, LastLogIndex: lastLogIndex, LastLogTerm: lastLogTerm}
				reply := &RequestVoteReply{}
				ok := rf.sendRequestVote(peer, args, reply)
				for !ok {
					ok = rf.sendRequestVote(peer, args, reply)
				}
				if ok {
					rf.mu.Lock()
					defer rf.mu.Unlock()
					if reply.VoteGranted && reply.Term == rf.currentTerm {
						//如果收到票了，并且票是当前任期的
						voteCnt++
						if voteCnt > len(rf.peers)/2 && rf.state == CANDIDATE {
							//成功当选
							Debug(dVote, "S%d win elect at T%d", rf.me, rf.currentTerm)
							rf.state = LEADER
							rf.matchIndex = make([]int, len(rf.peers))
							rf.nextIndex = make([]int, len(rf.peers))
							for i := range rf.nextIndex {
								rf.nextIndex[i] = len(rf.logs)
							}
							rf.heartbeatTimer.Reset(0)
						}
					} else if reply.Term > rf.currentTerm {
						//当前的任期不是最新的
						Debug(dVote, "S%d finds a raft peer S%d with term %v,so convert to follower", rf.me, peer, reply.Term)
						rf.currentTerm, rf.state, rf.votedFor = reply.Term, FOLLOWER, -1
					}
				}
			}(peer, rf.currentTerm)
		}
	}
}

// 更新nextIndex与matchIndex数组
func (rf *Raft) updateMatchIndexAndNextIndex(peer, index int) {
	rf.matchIndex[peer] = maxInt(rf.matchIndex[peer], index)
	rf.nextIndex[peer] = maxInt(rf.nextIndex[peer], index+1)
}

// 将新的日志添加到log中，同时向Follower广播日志新增消息
func (rf *Raft) broadcastLog(logEntry LogEntry) int {
	// 2B 传播日志添加的消息
	rf.logs = append(rf.logs, logEntry) //将日志条目添加到日志中
	log := rf.logs                      //复制一份当前的日志
	currentTerm := rf.currentTerm       //当前任期
	receiveCnt := 1                     //有多少个raft对等节点收到了新增的日志,初始化为1
	lastLogIndex := len(rf.logs) - 1    //当前日志记录的最后一条记录的下标
	successFlag := false
	Debug(dLeader, "S%d broadcast new logEntry {Command:%v,Term:%d} at LogIndex %d at T%d", rf.me, logEntry.Command, logEntry.Term, lastLogIndex, currentTerm)
	for peer := range rf.peers {
		if peer != rf.me {
			// 只要Peer将当前的日志条目logEntry
			prevLogIndex := rf.nextIndex[peer] - 1 //当前raft对等体中已经存在的下标，这个的值理应永远小于lastLogIndex
			prevLogTerm := rf.logs[prevLogIndex].Term
			commitIndex := rf.commitIndex
			go func(peer int) {
				args := AppendEntriesArgs{Term: currentTerm, //发送时的任期
					LeaderId:     rf.me, //当前rf的标识符
					PrevLogIndex: prevLogIndex,
					PrevLogTerm:  prevLogTerm,
					Entries:      log[prevLogIndex+1 : lastLogIndex+1], //将从prevLogIndex+1到当前最新的日志条目都发送给Follower
					LeaderCommit: commitIndex}                          //当前rf的提交下标
				ok := false
				for !ok {
					reply := AppendEntriesReply{}
					ok = rf.sendAppendEntries(peer, &args, &reply)
					if ok { // Follower接收者成功返回
						rf.mu.Lock()
						if reply.Term > currentTerm { // Follower的任期大于发送时的任期，直接结束
							if reply.Term > rf.currentTerm { // Follower的任期大于发送方的任期，更新任期与状态
								rf.currentTerm, rf.state, rf.votedFor = reply.Term, FOLLOWER, -1
							}
							rf.mu.Unlock()
							return
						}
						if reply.Success { // lastLogIndex以及之前的日志都已经被Follower接收了
							rf.updateMatchIndexAndNextIndex(peer, lastLogIndex)
							receiveCnt++
							// 大部分raft节点都接收到了日志条目,提交命令
							if receiveCnt > len(rf.peers)/2 && !successFlag {
								//在Leader节点上运行提交条目
								rf.commitIndex = maxInt(rf.commitIndex, lastLogIndex)
								rf.applyLogs(rf.commitIndex, rf.commitIndex)
								commitIndex = rf.commitIndex
								successFlag = true //保证只会提交一次
							}
							rf.mu.Unlock()
							return
						} else { //args.PrevLogIndex太大了
							args.PrevLogIndex = reply.ConflictTermFirstIndex - 1 //取前任任期下标
							args.PrevLogTerm = rf.logs[args.PrevLogIndex].Term
							args.Entries = rf.logs[args.PrevLogIndex+1 : lastLogIndex+1]
							args.LeaderCommit = rf.commitIndex
							Debug(dLeader, "S%d resend appendEntries to S%d at PrevLogIndex %d at T%d", rf.me, peer, args.PrevLogIndex, currentTerm)
							ok = false // 设为False 重新发送
						}
						rf.mu.Unlock()
					}
				}

			}(peer)
		}
	}
	return lastLogIndex
}

// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{
		peers:     peers,
		persister: persister,
		me:        me,
		applyCh:   applyCh,
	}
	// TODO (2A, 2B, 2C) Your initialization code here .
	// 初始化持久化的变量
	rf.currentTerm = 0
	rf.votedFor = -1
	rf.logs = make([]LogEntry, 1)
	rf.logs[0] = LogEntry{0, nil}
	// 初始化非持久化变量
	rf.state = FOLLOWER
	rf.commitIndex = 0
	rf.lastApplied = 0
	rf.electionTimer = time.NewTimer(randomizedElectionTimeout())
	rf.heartbeatTimer = time.NewTimer(HEARTBEATETIME)
	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// start ticker goroutine to start elections
	go rf.ticker()
	Debug(dInfo, "S%d raft peer create", rf.me)
	return rf
}
