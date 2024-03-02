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


### Part B log
### Part C persistence
### Part D log compaction

## 实验分析


## 实验结果
