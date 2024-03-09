# 实验三：Raft

## 实验介绍
这是一系列实验中的第一个，你将在其中构建一个可容错的K/V存储系统。在本实验中，你将实现Raft，一种复制状态机的协议。在下一个实验中，你将在Raft上构建一个K/V服务。然后，你将在做个复制的状态机上“分片”的服务，以获得更高性能。

复制的服务通过将其状态（即数据）的完整副本存储在多个副本服务器上来实现容错。复制允许服务继续运行，即使其某些服务器遇到故障（崩溃或网络损坏或片状）。挑战在于，故障可能会导致副本保存不同的数据副本。

Raft将客户端请求组织成一个序列（我们称作日志），并确保所有副本服务器能看到相同的日志。每个副本按照日志顺序执行客户端请求，并将他们应用于服务状态的本地副本。由于所有活动副本看到的日志内容都相同，因此它们都以相同的顺序执行相同的请求，因此他们的服务状态持续保持一致。如果一个服务器出现故障但后来恢复，Raft 会负责使其日志保持最新状态。只要至少大多数服务器还活着并且可以相互通信，Raft 就会继续运行。如果没有这样的多数，Raft 将不会取得任何进展，但一旦多数可以再次沟通，它就会从中断的地方继续前进。

在本实验中，你将 Raft 实现为具有相关方法的 Go 对象类型，旨在用作大型服务中的模块。一组 Raft 实例与 RPC 相互通信，以维护复制的日志。你的 Raft 接口将支持无限序列的编号命令，也称为日志条目。这些条目使用索引号进行编号。最终将提交具有给定索引的日志条目。此时，你的 Raft 应该将日志条目发送到更大的服务，以便执行。此时，你的 Raft 应该将日志条目发送到相应的服务，以便执行。

您应该遵循扩展的 Raft 论文中的设计，并特别注意图 2。您将实现论文中的大部分内容，包括保存持久状态并在节点发生故障后读取它，然后重新启动。您不会实施集群成员身份更改（第 6 节）。

## 实验要求

在`src/raft/raft.go`中，是raft的框架代码。`src/raft/test_test.go`中有评分代码。

若要启动并运行，可执行一下命令。不要忘记 git pull 获取最新的软件。
```bash
$ cd ~/6.5840
$ git pull
...
$ cd src/raft
$ go test
Test (3A): initial election ...
--- FAIL: TestInitialElection3A (5.04s)
        config.go:326: expected one leader, got none
Test (3A): election after network failure ...
--- FAIL: TestReElection3A (5.03s)
        config.go:326: expected one leader, got none
...
$
```

你必须实现下面四个接口
```go
// create a new Raft server instance:
// peers 一组raft节点（包括当前节点）的网络标识符
// me 是当前节点在peers的下标
rf := Make(peers, me, persister, applyCh)

// start agreement on a new log entry:
// start需要立即返回
rf.Start(command interface{}) (index, term, isleader)

// ask a Raft for its current term, and whether it thinks it is leader
rf.GetState() (term, isLeader)

// each time a new entry is committed to the log, each Raft peer
// should send an ApplyMsg to the service (or tester).
type ApplyMsg
```
### Part A leader election
***任务***: 实现raft leader选举与心跳(AppendEntries RPC不带任何日志条目就是在发送心跳)。使用`go test -run 3A`测试

遇到的难点：
锁的使用，因为在本次实验中很多地方都会使用到锁，所以怎样使用锁来保护临界区很难。主要参考[raft lock advice](https://pdos.csail.mit.edu/6.824/labs/raft-locking.txt)
实验本身已经给的框架中的ticker函数，使用time.Sleep()来实现计时器的效果。所以这里我遇到的第一个困难就是，在什么地方判断`voteCnt`大于等于`majority`，然后修改`raft`节点状态为Leader。

我的第一次实现如下
```go
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
```
该实现存在下面的问题
1. 请求投票的方式是顺序的，每向一个peer请求，就需要等待RPC，因此存在性能问题。必须要全部的请求都结束时，才可以统计voteCnt。
2. 存在数据竞争，rf.state可能会在appendEntries时遭遇更改，而正在举行投票的协程不知道，还在投票。
3. 不容易加锁，如果直接给ticker中的判断语句直接加一把大锁，那么就会导致各个竞选者互相卡死。因为在拉票的时候，不能处理请求拉票的请求。

最终的实现如下
```go
// 选举计时器
func (rf *Raft) electionTicker() {
	// 如果raft节点还在运行
	for !rf.killed() {
		// 检查是否应该举行选举
		rf.mu.Lock()
		if rf.state == CANDIDATE {
			// 上一次竞选失败，重新竞选
			Debug(dVote, "S%d lose last elect due to eletionTimeout,so S%d restart Elect", rf.me, rf.me)
			rf.startElect()
		} else if rf.state == FOLLOWER && time.Since(rf.lastTimeHeard) > 3*HEARTBEATETIME {
			// 已经3分钟没有听到810975的故事了，开始选举
			Debug(dVote, "S%d lose connect from Leader,so S%d start Elect", rf.me, rf.me)
			rf.startElect()
		}
		rf.mu.Unlock()
		// 随机计时器
		ms := 50 + (rand.Int63() % 300)
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
}

func (rf *Raft) startElect() {
	rf.state = CANDIDATE
	rf.currentTerm++ //任期自增
	rf.votedFor = rf.me
	voteCnt := 1 //自己投自己一票
	for peer := range rf.peers {
		if peer != rf.me {
			go func(peer int, term int) {
				//向其他人拉票
				args := &RequestVoteArgs{Term: term, CandidateId: rf.me}
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
```
相比前面的实现，此实现的优点在于。
1. 异步拉票，判断voteCnt的方法在每次收到票的时候进行
2. 容易实现锁，而且每一个锁锁住的资源符合锁建议，即锁住的资源不会占用很长的时间，都是CPU密集型的任务
#### 实验结果
```bashh
go test -run 3A
Test (3A): initial election ...
  ... Passed --   3.0  3  574  146148    0
Test (3A): election after network failure ...
  ... Passed --   4.5  3 1075  227143    0
Test (3A): multiple elections ...
  ... Passed --   5.5  7 4449  924553    0
PASS
ok      6.5840/raft     13.094s

#  数据格式注：
#				   时间 raft对等体  RPC请求数 RPC消息总字节数 raft提交的log数量
#  ... Passed --   5.5  7           4449      924553          0
```

### Part B log
实现 leader 和 follower 代码以追加新的日志条目，以通过` go test -run 3B `测试。

两个关键方法`broadcastLog`与`AppendEntries`如下
```go
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
	}
	// 条件5  If leaderCommit > commitIndex, set commitIndex = min(leaderCommit, index of last new entry)
	if args.LeaderCommit > rf.commitIndex {
		rf.commitIndex = minInt(args.LeaderCommit, len(rf.log)-1)
		for i := rf.lastApplied + 1; i <= rf.commitIndex; i++ {
			rf.applyCh <- ApplyMsg{CommandValid: true,
				Command:      rf.log[i].Command,
				CommandIndex: i}
				Debug(dFollower,"S%d apply Command %v at LogIndex %d",rf.me,rf.log[i].Command,i)
		}
		rf.lastApplied = rf.commitIndex
	}
	// 收到有效的Leader的更新，此时需要更新Leader与最后一次收到Leader的时间信息
	rf.currentTerm = args.Term
	rf.state = FOLLOWER
	rf.lastTimeHeard = time.Now()
	reply.Term, reply.Success = rf.currentTerm, true
}


// 将新的日志添加到log中，同时向Follower广播日志新增消息
func (rf *Raft) broadcastLog(logEntry LogEntry) int {
	// 2B 传播日志添加的消息
	rf.log = append(rf.log, logEntry) //将日志条目添加到日志中
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
			go func(peer int) {
				args := AppendEntriesArgs{Term: currentTerm, //发送时的任期
					LeaderId:     rf.me, //当前rf的标识符
					PrevLogIndex: prevLogIndex,
					PrevLogTerm:  prevLogTerm,
					Entries:      rf.log[prevLogIndex+1 : lastLogIndex+1], //将从prevLogIndex+1到当前最新的日志条目都发送给Follower
					LeaderCommit: rf.commitIndex}                          //当前rf的提交下标
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
							if rf.matchIndex[peer] < lastLogIndex {
								rf.matchIndex[peer] = lastLogIndex
							}
							if rf.nextIndex[peer] < lastLogIndex {
								rf.nextIndex[peer] = lastLogIndex + 1
							}
							receiveCnt++
							// 大部分raft节点都接收到了日志条目,提交命令
							if receiveCnt > len(rf.peers)/2 && !successFlag {
								//在Leader节点上运行提交条目
								rf.commitIndex = maxInt(rf.commitIndex, lastLogIndex)
								for i := rf.lastApplied + 1; i <= rf.commitIndex; i++ {
									rf.applyCh <- ApplyMsg{CommandValid: true,
										Command:      rf.log[i].Command,
										CommandIndex: i}
									Debug(dLeader,"S%d apply Command %v at LogIndex %d",rf.me,rf.log[i].Command,i)
								}
								rf.lastApplied = rf.commitIndex
								successFlag = true //保证只会提交一次
							}
							rf.mu.Unlock()
							return
						} else {
							//线性递减PrevLogIndex
							args.PrevLogIndex = maxInt(args.PrevLogIndex-1, 0)
							args.PrevLogTerm = rf.log[args.PrevLogIndex].Term
							args.Entries = rf.log[args.PrevLogIndex+1 : lastLogIndex+1]
							args.LeaderCommit = rf.commitIndex
						}
						rf.mu.Unlock()
					}
				}

			}(peer)
		}
	}
	return lastLogIndex
}
```
`broadcastLog`方法的实现与`StartElect`方法原理一致。
`AppendEntries`按照论文图2的描述来实现

#### 实验结果
第一次实验结果错误，与修改方法
```bash
$: go test -run 3B
Test (3B): basic agreement ...
  ... Passed --   0.1  3   33    8410    3
Test (3B): RPC byte count ...
  ... Passed --   0.3  3   75  159694   11
Test (3B): test progressive failure of followers ...
  ... Passed --   4.3  3  868  185841    3
Test (3B): test failure of leaders ...
  ... Passed --   4.4  3 1385  293943    3
Test (3B): agreement after follower reconnects ...  
# 错误原因：因为我只在新增条目的时候更新nextIndex，超出了时间限制。
# 于是我控制在发送心跳的时候同时更新nextIndex。
--- FAIL: TestFailAgree3B (11.88s)
    config.go:601: one(106) failed to reach agreement
Test (3B): no agreement if too many followers disconnect ...
  ... Passed --   7.2  5 1935  464797    2
Test (3B): concurrent Start()s ...
  ... Passed --   0.6  3   95   25135    6
Test (3B): rejoin of partitioned leader ...  
# 错误原因：我一开始的原因不知道为什么了，但是在我修复第一个错误后
# 发现是因为我每次只要更新Follower的CommitIndex的时候，都会去apply条目。（而此时的条目是错误的）
# 修改方法是新建一个变量matchIndex，保证如果没有entries条目到来，它就等于args.PrevLogIndex
# 如果有entries条目到来，它在等于len(rf.log)-1
# 这样做保证了，不会错误地apply
--- FAIL: TestRejoin3B (12.49s)
    config.go:601: one(103) failed to reach agreement
Test (3B): leader backs up quickly over incorrect follower logs ...
--- FAIL: TestBackup3B (13.59s)
    config.go:601: one(1320326507008556142) failed to reach agreement
Test (3B): RPC counts aren't too high ... 
# 这里出错很可能是因为心跳间隔设置太小了，我一开始的心跳间隔设置在10ms，后来改成了50ms就通过了，不然我的心跳占用了太多RPC了
--- FAIL: TestCount3B (19.53s) 
    test_test.go:658: term changed too often
FAIL
exit status 1
FAIL    6.5840/raft     74.589s
```
上述错误修改后，可以一次性通过测试。（暂时还未使用dTest.py进行循环测试）
```bash
$ go test -run 3B
Test (3B): basic agreement ...
  ... Passed --   0.2  3   20    4830    3
Test (3B): RPC byte count ...
  ... Passed --   0.7  3   54  113954   11
Test (3B): test progressive failure of followers ...
  ... Passed --   4.2  3  235   45504    3
Test (3B): test failure of leaders ...
  ... Passed --   4.5  3  363   74664    3
Test (3B): agreement after follower reconnects ...
  ... Passed --   5.0  3  229   56117    8
Test (3B): no agreement if too many followers disconnect ...
  ... Passed --   3.5  5  436   83545    4
Test (3B): concurrent Start()s ...
  ... Passed --   0.7  3   39   10298    6
Test (3B): rejoin of partitioned leader ...
  ... Passed --   3.7  3  260   57967    4
Test (3B): leader backs up quickly over incorrect follower logs ...
labgob warning: Decoding into a non-default variable/field Term may not work # 暂时不知道这个错误的原因
  ... Passed --  26.6  5 7976 3026914  104
Test (3B): RPC counts aren't too high ...
  ... Passed --   2.3  3  106   28756   12
PASS
ok      6.5840/raft     51.371s
```
### Part C persistence
### Part D log compaction

## 测试文件分析

```go 

// 调用一次Start,要求调用后2秒内，期望的服务器数量都提交了日志，否则就会再调用一遍Start。
cfg.one() 
```


