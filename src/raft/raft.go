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

const HEARTBEATETIME = time.Duration(50) * time.Millisecond //心跳发送时间间隔

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
	log         []LogEntry //日志条目；每个条目包含命令和从领导者收到条目的任期，索引从1开始

	//非持久化的变量

	lastTimeHeard time.Time     //最后一次收到Leader的消息的时间戳，初始化为time.Unix(0, 0)
	state         RaftState     //表示当前节点状态
	commitIndex   int           //已知的已提交的最高日志条目的索引（初始化为 0，单调递增）
	lastApplied   int           //应用于状态机的最高日志条目的索引（初始化为 0，单调递增）
	applyCh       chan ApplyMsg //当日志条目提交时，向管道写入ApplyMsg
	//非持久化的Leader使用的变量

	nextIndex  []int //对于每个服务器，要发送到该服务器的下一条日志条目的索引（初始化为领导者的最后一条日志索引 + 1）
	matchIndex []int //在每个服务器上已知已经复制的最高日志条目的索引（初始化为 0，单调递增）
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
	rf.lastTimeHeard = time.Now() //更新接收的时间，免得自己阻止投票
	Debug(dVote, "S%d Granting Vote to S%d at T%d", rf.me, args.CandidateId, args.Term)
}

// 检查请求的日志是否比当前的新,如果是最新的，返回true，否则返回false
func (rf *Raft) isLogUpToDate(lastLogTerm, lastLogIndex int) bool {
	curLastLogIndex := len(rf.log) - 1          //日志记录的最后一条记录的下标
	curLastLogTerm := rf.log[curLastLogIndex].Term //日志记录的最后一条记录的任期
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
	Term    int  //当前raft节点的任期，可用于leader更新
	Success bool //如果follower包含匹配PrevLogIndex与PrevLogTerm值的条目，则为true
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	// TODO A,B
	rf.mu.Lock()
	defer rf.mu.Unlock()
	// 条件1 $5.1
	if args.Term < rf.currentTerm {
		reply.Term, reply.Success = rf.currentTerm, false
		return
	}
	// 条件2 Reply false if log doesn’t contain an entry at prevLogIndex whose term matches prevLogTerm (§5.3)
	// 如果下标不在当前rf.log范围中，那说明当前log不包含，可以直接返回
	// 如果下标在当前rf.log范围中，对应下标的log条目任期不符合符合Leader的log对应任期，直接返回
	if args.PrevLogIndex >= len(rf.log) || rf.log[args.PrevLogIndex].Term != args.PrevLogTerm {
		Debug(dFollower, "S%d can not match S%d's PrevLogIndex %d", rf.me, args.LeaderId, args.PrevLogIndex)
		reply.Term, reply.Success = rf.currentTerm, false
		rf.currentTerm = args.Term
		rf.state = FOLLOWER
		rf.lastTimeHeard = time.Now()
		return
	}
	matchIndex := args.PrevLogIndex
	// 条件3与条件4
	// 非心跳请求,更新日志条目
	if len(args.Entries) != 0 {
		nextIndex := args.PrevLogIndex + 1 //从与Leader有相同的日志部分开始的下一个下标
		prevLogLength := len(rf.log)       //当前的日志长度
		Debug(dFollower, "S%d receive S%d's log from %d to %d", rf.me, args.LeaderId, nextIndex, nextIndex+len(args.Entries)-1)
		for _, logEntry := range args.Entries {
			// 日志覆盖
			if nextIndex < prevLogLength {
				rf.log[nextIndex] = logEntry
				nextIndex++
			} else {
				// 日志新增
				rf.log = append(rf.log, logEntry)
			}
		}
		matchIndex = len(rf.log) - 1
	}
	// 条件5  If leaderCommit > commitIndex, set commitIndex = min(leaderCommit, index of last new entry)
	// 我在条件五更新时，将日志提交，但此时有一个前提必须满足。也就是所有要应用的日志条目必须与Leader相同。
	if args.LeaderCommit > rf.commitIndex {
		rf.commitIndex = minInt(args.LeaderCommit, len(rf.log)-1)
		cnt := 0
		for i := rf.lastApplied + 1; i <= rf.commitIndex && i <= matchIndex; i++ {
			rf.applyCh <- ApplyMsg{CommandValid: true,
				Command:      rf.log[i].Command,
				CommandIndex: i}
			Debug(dFollower, "S%d apply {Command:%v,Term:%d} at LogIndex %d", rf.me, rf.log[i].Command, rf.log[i].Term, i)
			cnt++
		}
		rf.lastApplied = rf.lastApplied + cnt
	}
	// 收到有效的Leader的更新，此时需要更新Leader与最后一次收到Leader的时间信息
	rf.currentTerm = args.Term
	rf.state = FOLLOWER
	rf.lastTimeHeard = time.Now()
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

// 心跳计时器
func (rf *Raft) heartbeats() {
	for !rf.killed() {
		rf.mu.Lock()
		if rf.state != LEADER {
			//已经不再是Leader了，不用执行心跳了，记得把锁释放
			rf.mu.Unlock()
			return
		}

		leaderCommit := rf.commitIndex
		for peer := range rf.peers {
			if peer == rf.me {
				continue
			}
			prevLogIndex := rf.nextIndex[peer] - 1
			prevLogTerm := rf.log[prevLogIndex].Term
			go func(peer int, currentTerm int) {
				// 心跳的话，发送的Entries条目为空
				args := AppendEntriesArgs{Term: currentTerm, LeaderId: rf.me, PrevLogIndex: prevLogIndex, PrevLogTerm: prevLogTerm, Entries: nil, LeaderCommit: leaderCommit}
				reply := AppendEntriesReply{}
				// Debug(dLeader,"S%d appendEntry to S%d with prevLogIndex %d",rf.me,peer,prevLogIndex)
				ok := rf.sendAppendEntries(peer, &args, &reply)
				if ok {
					if reply.Term > currentTerm {
						return
					}
					rf.mu.Lock()
					defer rf.mu.Unlock()
					if !reply.Success {
						rf.nextIndex[peer] = maxInt(prevLogIndex, rf.matchIndex[peer]+1)
					}
				}
			}(peer, rf.currentTerm)
		}
		//每周期执行一次心跳
		rf.mu.Unlock()
		time.Sleep(HEARTBEATETIME)
	}
}

// 选举计时器
func (rf *Raft) electionTicker() {
	// 如果raft节点还在运行

	for !rf.killed() {
		// TODO (2A) Your code here
		// Check if a leader election should be started.
		rf.mu.Lock()
		if rf.state == CANDIDATE {
			//上一次竞选失败，重新竞选
			Debug(dVote, "S%d lose last elect due to eletionTimeout,so S%d restart Elect", rf.me, rf.me)
			rf.startElect()
		} else if rf.state == FOLLOWER && time.Since(rf.lastTimeHeard) > 3*HEARTBEATETIME {
			//已经3分钟没有听到810975是什么了，开始选举
			Debug(dVote, "S%d lose connect from Leader,so S%d start Elect", rf.me, rf.me)
			rf.startElect()
		}
		rf.mu.Unlock()
		// pause for a random amount of time between 50 and 350
		// milliseconds.
		ms := 50 + (rand.Int63() % 300)
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
}

func (rf *Raft) startElect() {
	rf.state = CANDIDATE
	rf.currentTerm++ //任期自增
	rf.votedFor = rf.me
	voteCnt := 1 //自己投自己一票
	// 使用golang的闭包，所以不用go func的时候不用传输参数
	lastLogTerm := rf.log[len(rf.log)-1].Term //日志记录的最后一条记录的任期
	lastLogIndex := len(rf.log) - 1           //日志记录的最后一条记录的下标
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
							Debug(dVote, "S%d win elect with T%d", rf.me, rf.currentTerm)
							rf.state = LEADER
							rf.matchIndex = make([]int, len(rf.peers))
							rf.nextIndex = make([]int, len(rf.peers))
							for i := range rf.nextIndex {
								rf.nextIndex[i] = len(rf.log)
							}
							go rf.heartbeats()
						}
					} else if reply.Term > rf.currentTerm {
						//当前的任期不是最新的
						Debug(dVote, "S%d finds a raft peer S%d with term %v,so convert to follower", rf.me, peer, reply.Term)
						rf.currentTerm = reply.Term
						rf.state = FOLLOWER
						rf.votedFor = -1
					}
				}
			}(peer, rf.currentTerm)
		}
	}
}

// 将新的日志添加到log中，同时向Follower广播日志新增消息
func (rf *Raft) broadcastLog(logEntry LogEntry) int {
	// 2B 传播日志添加的消息
	rf.log = append(rf.log, logEntry) //将日志条目添加到日志中
	log := rf.log                     //复制一份当前的日志
	currentTerm := rf.currentTerm     //当前任期
	receiveCnt := 1                   //有多少个raft对等节点收到了新增的日志,初始化为1
	lastLogIndex := len(rf.log) - 1   //当前日志记录的最后一条记录的下标
	successFlag := false
	Debug(dLeader, "S%d start broadcast new logEntry {Command:%v,Term:%d} at LogIndex %d at Term %d", rf.me, logEntry.Command, logEntry.Term, lastLogIndex, currentTerm)
	for peer := range rf.peers {
		if peer != rf.me {
			// 只要Peer将当前的日志条目logEntry
			prevLogIndex := rf.nextIndex[peer] - 1 //当前raft对等体中已经存在的下标，这个的值理应永远小于lastLogIndex
			prevLogTerm := rf.log[prevLogIndex].Term
			commitIndex := rf.commitIndex
			go func(peer int) {

				args := AppendEntriesArgs{Term: currentTerm, //发送时的任期
					LeaderId:     rf.me, //当前rf的标识符
					PrevLogIndex: prevLogIndex,
					PrevLogTerm:  prevLogTerm,
					Entries:      log[prevLogIndex+1 : lastLogIndex+1], //将从prevLogIndex+1到当前最新的日志条目都发送给Follower
					LeaderCommit: commitIndex}                          //当前rf的提交下标
				reply := AppendEntriesReply{}
				ok := false
				for !ok {
					ok = rf.sendAppendEntries(peer, &args, &reply)
					// Follower接收者成功返回
					if ok {
						rf.mu.Lock()
						// Follower的任期大于发送时的任期，
						if reply.Term > currentTerm {
							// Follower的任期大于发送方的任期，更新任期与状态
							if reply.Term > rf.currentTerm {
								rf.currentTerm, rf.state = reply.Term, FOLLOWER
							}
							rf.mu.Unlock()
							return
						}
						// lastLogIndex以及之前的日志都已经被Follower接收了
						if reply.Success {
							if rf.matchIndex[peer] <= lastLogIndex {
								rf.matchIndex[peer] = lastLogIndex
							}
							if rf.nextIndex[peer] <= lastLogIndex {
								rf.nextIndex[peer] = lastLogIndex + 1
							}
							receiveCnt++
							// 大部分raft节点都接收到了日志条目,提交命令
							if receiveCnt > len(rf.peers)/2 && !successFlag {
								//在Leader节点上运行提交条目
								rf.commitIndex = maxInt(rf.commitIndex, lastLogIndex)
								commitIndex = rf.commitIndex
								cnt := 0
								for i := rf.lastApplied + 1; i <= rf.commitIndex; i++ {
									rf.applyCh <- ApplyMsg{CommandValid: true,
										Command:      rf.log[i].Command,
										CommandIndex: i}
									Debug(dLeader, "S%d apply {Command:%v,Term:%d} at LogIndex %d", rf.me, rf.log[i].Command, rf.log[i].Term, i)
									cnt++
								}
								rf.lastApplied = rf.lastApplied + cnt
								successFlag = true //保证只会提交一次
							}
							rf.mu.Unlock()
							return
						} else {
							//线性递减PrevLogIndex
							rf.nextIndex[peer] = args.PrevLogIndex
							args.PrevLogIndex = maxInt(args.PrevLogIndex-1, 0)
							args.PrevLogTerm = rf.log[args.PrevLogIndex].Term
							args.Entries = rf.log[args.PrevLogIndex+1 : lastLogIndex+1]
							args.LeaderCommit = rf.commitIndex
							Debug(dLeader, "S%d resend appendEntries to S%d with PrevLogIndex %d at Term %d", rf.me, peer, args.PrevLogIndex, currentTerm)
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
	rf.log = make([]LogEntry, 1)
	rf.log[0] = LogEntry{0, nil}
	// 初始化非持久化变量
	rf.lastTimeHeard = time.Unix(0, 0)
	rf.state = FOLLOWER
	rf.commitIndex = 0
	rf.lastApplied = 0
	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// start ticker goroutine to start elections
	go rf.electionTicker()
	Debug(dInfo, "S%d raft peer create", rf.me)
	return rf
}
