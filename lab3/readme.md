# 实验三：Raft

## 实验介绍

动手实现Raft

## 实验难点分析

### Part A leader election(moderate)
**任务**: 实现raft leader选举与心跳(AppendEntries RPC中的日志条目设为空就是在发送心跳)。使用`go test -run 3A`测试

难点：
1. 锁的使用，因为在本次实验中很多地方都会使用到锁，所以怎样使用一把锁来保护临界区很难。这里需要仔细的阅读[raft lock advice](https://pdos.csail.mit.edu/6.824/labs/raft-locking.txt)。我的总结是保证锁住的临界区不会执行耗时的代码，比如发送RPC。
2. 实验本身已经给的框架中的ticker函数，使用time.Sleep()来实现计时器的效果，但是我觉得使用time.Timer计时器会更加直观（虽然实验提示认为timer比较难使用）

建议：
对于举行选举的代码，我使用如下的模板进行实现。
```go
func startElect(){
	rf.mu.Lock()
	rf.currentTerm += 1 //自增任期
	rf.state = CANDIDATE //改变状态
	rf.votedFor = rf.me
	voteCnt := 1
	for <each peer> {
		go func(peer,term int) {
			// 配置args,reply参数
			ok := Call("Raft.RequestVote", &args, ...)
			// 处理回复
			if voteCnt > len(rf.peers)/2 && rf.state == CANDIDATE{ // 选举成功
				rf.state = LEADER //更新状态
				for i := range rf.nextIndex {
					rf.nextIndex[i] = len(rf.logs)
					rf.matchIndex[i] = 0
				}
				rf.heartbeatTimer.Reset(0) // 立刻触发心跳计时器
			}
		} (peer,rf.currentTerm)
	}
	rf.mu.Unlock()
}
```
对于如何处理Candidate发送过来的拉票，我使用如下的模板进行实现
```go
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	rf.mu.Lock() //首先进行加锁，保证并发的安全性
	defer rf.mu.Unlock()
	if args.Term < rf.currentTerm || (args.Term == rf.currentTerm && rf.votedFor != -1 && rf.votedFor != args.CandidateId) {
		reply.Term, reply.VoteGranted = rf.currentTerm, false
		return
	}
	if args.Term > rf.currentTerm {
		rf.state = FOLLOWER
		rf.currentTerm, rf.votedFor = args.Term, -1
	}
	if !rf.isLogUpToDate(args.LastLogTerm, args.LastLogIndex) {
		reply.Term, reply.VoteGranted = rf.currentTerm, false
		return
	}
	rf.votedFor = args.CandidateId
	reply.Term, reply.VoteGranted = rf.currentTerm, true
	rf.electionTimer.Reset(randomizedElectionTimeout())
	Debug(dVote, "S%d Granting Vote to S%d at T%d", rf.me, args.CandidateId, args.Term)
}
```

### Part B log replication(hard)
**任务**: 实现raft的日志复制。

难点：
1. 需要严格按照raft论文的图2进行实现，一定要优先保证论文中的所有条件满足。
2. 心跳超时时间与选举超时时间的值怎样设置很关键，是能否高概率通过`TestCount3B`与`TestBackup3B`的关键。如果心跳超时时间太短，那么触发的RPC就会太多，就无法通过`TestCount3B`，如果选举超时时间太短，就很容易反复选举不成功。（需要注意cfg.one每次进行一次start都要求在10秒中内得到结果，如果反复选举不成功就会超时，所以不妨将超时时间设的大一点）
3. 当Follower在与Leader进行日志复制时，如果发现冲突，一定要删除冲突的条目以及之后的条目。这是通过`TestRejoin3B`的关键

建议：
对于发送日志复制，与选举的模板差不多。
```go
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
```

错例分析：
下面这部分代码在FOLLOWER上进行日志复制，每次执行的流程是已经存在的就覆盖，不存在的就新增。
```go
if len(args.Entries) != 0 {
	nextIndex := args.PrevLogIndex + 1 //从与Leader有相同的日志部分开始的下一个下标
	prevLogLength := len(rf.log)       //当前的日志长度
	for _, logEntry := range args.Entries {
		if nextIndex < prevLogLength { // 日志覆盖
			rf.logs[nextIndex] = logEntry
			nextIndex++
		}  else { // 日志新增
			rf.logs = append(rf.logs, logEntry)
		}
	}
	matchIndex = len(rf.log) - 1
}
```
上面这部分代码没有严格遵守AppendEntries的第三个条件，因此他会存在部分概率无法通过`TestRejoin3B`

最终实验结果
```bash
$ VERBOSE=1 python3 dTest.py 3B -n 300
Failed test 3B - 20240312_183720/3B_69.log
Failed test 3B - 20240312_183720/3B_127.log
Failed test 3B - 20240312_183720/3B_147.log
Failed test 3B - 20240312_183720/3B_149.log
Failed test 3B - 20240312_183720/3B_199.log
┏━━━━━━┳━━━━━━━━┳━━━━━━━┳━━━━━━━━━━━━━━┓
┃ Test ┃ Failed ┃ Total ┃         Time ┃
┡━━━━━━╇━━━━━━━━╇━━━━━━━╇━━━━━━━━━━━━━━┩
│ 3B   │      5 │   300 │ 65.90 ± 7.99 │
└──────┴────────┴───────┴──────────────┘
出错基本都出在TestFailNoAgree3B的第一次达成一致，需要在两秒内达成，然后偶尔会倒霉的超时。这里还有优化空间
```


#### replicator优化
看了[@OneSizeFitsQuorum](https://github.com/OneSizeFitsQuorum)的[Lab文档](https://github.com/OneSizeFitsQuorum/MIT6.824-2021)的文档，使用replicator重构后的实验结果。当然500次不出错主要是我觉得主要是调整了选举时间，我将选举时间从原来的`150+[0-600]`调整为`300+[0-600]`。但是replicator显然相比之前的写法是更加优越的，因为在原来的实现中，每有一次日志新增在我的实现中就会多`n=len(peers)`个协程，每个协程各自发送RPC，这样就导致会发送大量的RPC，并且多个协程之间相互抢占CPU，导致资源浪费。使用Replicator模型的话，对于Leader来说，只会有n-1个协程负责发送数据，而且可以做到在一次RPC中发送多个Log。

此外在原来的实现中，对于Leader，只会在收到Start的时候，并且新日志提交了，才会apply日志，这在实现持久化的时候很不方便。
而在replicator模型中，会为每一个Server维护一个Applier协程，只要当前的`matchIndex[rf.me]`增长，就可以主动触发applier协程应用日志，这在未来实现持久化上会方便很多。
```bash
$ python3 dTest.py -p 2 -n 1000 3B
┏━━━━━━┳━━━━━━━━┳━━━━━━━┳━━━━━━━━━━━━━━┓
┃ Test ┃ Failed ┃ Total ┃         Time ┃
┡━━━━━━╇━━━━━━━━╇━━━━━━━╇━━━━━━━━━━━━━━┩
│ 3B   │      0 │   582 │ 56.96 ± 3.05 │
└──────┴────────┴───────┴──────────────┘
```
错例分析：
在replicator中，对于Leader的commitIndex更新，我错误地使用了二分，想要快速优化，但是这是有问题的，因为根据raft论文给出的条件，commitIndex不满足二分所需的单调性。
这个问题导致我的持久化卡了很久，可以仔细思考下面的实现。
```go
// 判断是否大多数节点收到了N
func (rf *Raft) hasBeenAppendedMajority(N int) bool {
	cnt := 0
	for _, v := range rf.matchIndex {
		if v >= N {
			cnt++
		}
	}
	//不满足二分的主要原因就在于rf.Log[N].Term == rf.CurrentTerm该等式。
	return cnt > len(rf.peers)/2 && rf.Log[N].Term == rf.CurrentTerm 
}

//处理日志复制响应的函数
func (rf *Raft) handleReplicateOneRoundResponse(peer int, args *AppendEntriesArgs, reply *AppendEntriesReply) {
	// 前略
	if reply.Success { //发出去的日志已经被接收了
		rf.matchIndex[peer] = args.PrevLogIndex + len(args.Entries)
		rf.nextIndex[peer] = args.PrevLogIndex + len(args.Entries) + 1
		left, right := rf.commitIndex+1, rf.getLastLog().Index
		Debug(dLeader,"S%d know S%d receive %v l:%d,r:%d",rf.me,peer,args.Entries,left,right)
		for left <= right {
			mid := (left + right) / 2
			Debug(dLeader,"S%d check mid:%d",rf.me,mid)
			if rf.hasBeenAppendedMajority(mid) {
				Debug(dLeader,"S%d check mid:%d success",rf.me,mid)
				rf.commitIndex = maxInt(rf.commitIndex, mid)
				rf.applyCond.Signal()
				left++
			} else {
				Debug(dLeader,"S%d check mid:%d fail",rf.me,mid)
				right--
			}
		}
	} else { //更新nextIndex
		//略
	}
}

```
下面给出输出日志，参考输出不难发现问题所在。由于添加的新日志在3，导致验证2失败，因为日志不在当前任期，无法提交。
```bash
S0 readPersist T1 S4 [{0                                                                                                               
<nil> 0} {1 11 1}]                                                                                                                     
S0 raft node created                                                                                                                   
                            S1 readPersist T1 S4 [{0                                                                                   
                            <nil> 0} {1 11 1}]                                                                                         
                            S1 raft node created  
													   S2 readPersist T2 S2 [{0                                                        
                                                       <nil> 0} {1 11 1} {1 12                                                         
                                                       2}]                                                                             
                                                       S2 raft node created
													   S2 win elect at T5 and                                                          
                                                       commitIndex:0,lastLogInde…                                                      
                                                       S2 broadcast HeartBeat at                                                       
                                                       T5 with commitIndex 0                                                           
                            S1 persist at                                                                                              
                            T5,votedFor:S2,lastLog:{{1                                                                                 
                            11 1}}                                                                                                     
S0 persist at                                                                                                                          
T5,votedFor:S2,lastLog:{{1                                                                                                             
11 1}}                                                                                                                                 
                                                       S2 append {5 13 3} at T5                                                        
                                                       S2 persist at                                                                   
                                                       T5,votedFor:S2,lastLog:{{5                                                      
                                                       13 3}}                                                                          
                                                       S2 broadcast Log at T5                                                          
                                                       S2 send Log to S1                                                               
                                                       prevLogIndex:2 at T5                                                            
                            S1 persist at                                                                                              
                            T5,votedFor:S2,lastLog:{{1                                                                                 
                            11 1}}                                                                                                     
                                                       S2 send Log to S4                                                               
                                                       prevLogIndex:2 at T5                                                            
                                                       S2 send Log to S3                                                               
                                                       prevLogIndex:2 at T5                                                            
                                                       S2 know Confilct with S1                                                        
                                                       S2 send Log to S1                                                               
                                                       prevLogIndex:1 at T5                                                            
                            S1's lastLog {1 12 2} at                                                                                   
                            T5                                                                                                         
                                                       S2 send Log to S0                                                               
                                                       prevLogIndex:2 at T5                                                            
                            S1's lastLog {5 13 3} at                                                                                   
                            T5                                                                                                         
                            S1 persist at                                                                                              
                            T5,votedFor:S2,lastLog:{{5                                                                                 
                            13 3}}                                                                                                     
                                                       S2 know S1 receive [{1 12                                                       
                                                       2} {5 13 3}] l:1,r:3                                                            
                                                       S2 check mid:2                                                                  
                                                       S2 check mid:2 fail                                                             
                                                       S2 check mid:1                                                                  
                                                       S2 check mid:1 fail                                                             
S0 persist at                                                                                                                          
T5,votedFor:S2,lastLog:{{1                                                                                                             
11 1}}                                                                                                                                 
                                                       S2 check mid:1                                                                  
                                                       S2 check mid:1 fail                                                             
                                                       S2 know Confilct with S0                                                        
                                                       S2 send Log to S0                                                               
                                                       prevLogIndex:1 at T5                                                            
S0's lastLog {1 12 2} at T5                                                                                                            
S0's lastLog {5 13 3} at T5                                                                                                            
S0 persist at                                                                                                                          
T5,votedFor:S2,lastLog:{{5                                                                                                             
13 3}}                                                                                                                                 
                                                       S2 know S0 receive [{1 12                                                       
                                                       2} {5 13 3}] l:1,r:3                                                            
                                                       S2 check mid:2                                                                  
                                                       S2 check mid:2 fail                                                             
                                                       S2 check mid:1                                                                  
                                                       S2 check mid:1 fail                                                             
                                                       S2 check mid:1                                                                  
                                                       S2 check mid:1 fail   
```
因此可知，想要优化raft还是需要更加仔细的思考才行。
### Part C persistence
持久化，... 没做什么优化，基本就是currentTerm，votedFor，log有变化就更新。
暂时WAL技术不太懂，如果用WAL技术的话，可优化的地方应该挺多的

当前实验结果
```bash
$ python3 dTest.py 3C
Failed test 3C - 20240322_201004/3C_5.log
Failed test 3C - 20240322_201004/3C_6.log
┏━━━━━━┳━━━━━━━━┳━━━━━━━┳━━━━━━━━━━━━━━━━┓
┃ Test ┃ Failed ┃ Total ┃           Time ┃
┡━━━━━━╇━━━━━━━━╇━━━━━━━╇━━━━━━━━━━━━━━━━┩
│ 3C   │      2 │    10 │ 203.84 ± 10.74 │
└──────┴────────┴───────┴────────────────┘
```
### Part D log compaction

## 参考实现
[@OneSizeFitsQuorum](https://github.com/OneSizeFitsQuorum)的[Lab文档](https://github.com/OneSizeFitsQuorum/MIT6.824-2021)
