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

### Part C persistence
### Part D log compaction

## 实验分析




