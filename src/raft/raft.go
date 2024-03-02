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
	CommandIndex int

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
	curLeader      int       //当前的Leader，可用于重定向client，初始化为-1
	lastUpdateTime time.Time //最后一次收到Leader的消息的时间戳，初始化为time.Unix(0, 0)
	state          RaftState //表示当前节点状态
	commitIndex    int       //已知的已提交的最高日志条目的索引（初始化为 0，单调递增）
	lastApplied    int       //应用于状态机的最高日志条目的索引（初始化为 0，单调递增）

	//非持久化的Leader使用的变量
	nextIndex  []int //对于每个服务器，要发送到该服务器的下一条日志条目的索引（初始化为领导者的上一条日志索引 + 1）
	matchIndex []int //已知在服务器上复制的最高日志条目的索引（初始化为 0，单调递增）
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

	var term int
	var isleader bool
	// TODO (2A) Your code here .
	term = rf.currentTerm
	isleader = (rf.state == LEADER)
	return term, isleader
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

	if args.Term < rf.currentTerm {
		// 候选者的任期小于当前服务器的任期，拒绝请求
		reply.VoteGranted = false
	} else if rf.currentTerm == args.Term {
		// 候选者的任期等于当前服务器的任期
		// 可能的情况：1) 同一任期的候选人互相拉票,而显然他们不会互相投票，因此返回False
		//             2) ...?
		if rf.votedFor != args.CandidateId {
			reply.VoteGranted = false
		} else {
			reply.VoteGranted = true
		}
	} else {
		//投票之前需要更新当前任期,因为是第一次更新，所以当前raft节点必然还没有投过票，因此直接投票
		rf.currentTerm = args.Term
		rf.votedFor = args.CandidateId
		reply.VoteGranted = true
		Debug(dVote, "S%d Granting Vote to S%d at T%d", rf.me, args.CandidateId, args.Term)
	}
	reply.Term = rf.currentTerm
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
	Entries      []LogEntry //
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
	if args.Term < rf.currentTerm {
		reply.Success = false
		return
	}
	// 收到有效的Leader的更新，此时需要更新Leader与最后一次收到Leader的时间信息
	rf.curLeader = args.LeaderId
	rf.state = FOLLOWER
	rf.lastUpdateTime = time.Now()
	rf.votedFor = -1
	//条件2,3,4,5先略
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

// 向Follower发送心跳
func (rf *Raft) heartbeats() {
	for !rf.killed() && rf.state == LEADER {
		for peer := range rf.peers {
			if peer == rf.me {
				continue
			}
			args := AppendEntriesArgs{Term: rf.currentTerm, LeaderId: rf.me}
			reply := AppendEntriesReply{}
			rf.sendAppendEntries(peer, &args, &reply)
		}
		time.Sleep(time.Duration(10) * time.Millisecond)
	}
}

func (rf *Raft) ticker() {
	// 如果raft节点还在运行

	for !rf.killed() {
		// pause for a random amount of time between 50 and 350
		// milliseconds.
		ms := 50 + (rand.Int63() % 300)
		time.Sleep(time.Duration(ms) * time.Millisecond)

		// TODO (2A) Your code here
		// Check if a leader election should be started.
		if rf.state == CANDIDATE || (rf.state == FOLLOWER && rf.isLossConn()) {
			Debug(dInfo, "S%d start Elect", rf.me)
			// raft节点当前无法与leader相连,则进行一次选举
			if rf.startElect() {
				//如果成功当选Leader，通知所有peer
				Debug(dInfo, "S%d win Elect", rf.me)
				go rf.heartbeats()
			}
		}
	}
}

func (rf *Raft) isLossConn() bool {
	// TODO A 检查与Leader的连接是否正常
	// 如果30ms没有接收到Leader发送过来的请求，就返回False
	return time.Since(rf.lastUpdateTime) > time.Duration(30)*time.Millisecond
}

// raft节点无法连接Leader，因此它要进行选举
func (rf *Raft) startElect() bool {
	// TODO A 进行选举
	rf.currentTerm++
	rf.state = CANDIDATE
	rf.votedFor = rf.me
	voteCnt := 1
	for peer := range rf.peers {
		if peer == rf.me {
			continue
		}
		args := RequestVoteArgs{Term: rf.currentTerm, CandidateId: rf.me}
		reply := RequestVoteReply{}
		ok := rf.sendRequestVote(peer, &args, &reply)
		if ok {
			if reply.Term > rf.currentTerm {
				//如果发现了新的任期
				rf.curLeader = reply.Term
				rf.state = FOLLOWER
				return false
			} else if reply.VoteGranted {
				Debug(dVote, "S%d <- S%d Got vote", rf.me, peer)
				voteCnt++
			}
		}
	}
	if voteCnt > len(rf.peers)/2 {
		rf.state = LEADER
		return true
	} else {
		return false
	}
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
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// TODO (2A, 2B, 2C) Your initialization code here .
	// 初始化持久化的变量
	rf.currentTerm = 0
	rf.votedFor = -1
	rf.log = make([]LogEntry, 1)
	// 初始化非持久化变量

	rf.curLeader = -1
	rf.lastUpdateTime = time.Unix(0, 0)
	rf.state = FOLLOWER
	rf.commitIndex = 0
	rf.lastApplied = 0
	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// start ticker goroutine to start elections
	go rf.ticker()
	Debug(dInfo, "S%d raft peer create", rf.me)
	return rf
}
