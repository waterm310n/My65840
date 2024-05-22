# 实验四：容错的K/V服务

客户端可以使用下面三种RPC

`Put(key,value)`:替换数据库中特定键的值
`Append(key,value)`:将arg追加到key的值(如果键不存在，则将现有值视为空字符串)
`Get(key)`:获取当前键的值(对于不存在的键返回空字符串)

键和值都是字符串。请注意，与实验 2 不同， Put 与 Append 不应向客户端返回值。

需要修改`src/kvraft`中的`kvraft/client.go`,`kvraft/server.go`,`kvraft/common.go`

## 4A Key/value service without snapshots

实现一个没有快照的容错的K/V服务。

### 实验难点分析

隐藏条件：**单个Client向Server发送RPC是串行的，Server接收的请求是并发的** 这个条件后者很显然，前者我看了博客才知道。前者困扰我的地方是，如果单个Client向Server发送请求，同时是并行的，我在之前的实验二中，单服务器的K/V Server的实现就是这样想的。所以我当时的实现是，对于每一个请求我都不分ClientId，而是一个请求一个SequenceNum，这样保证去重。然后为了避免不会占用过大的空间，我让Client在每一个请求之后，发送一个Delete RPC，用于删除先前的SequenceNum。这个实现导致我的实验二的效率很低。

而如果有了隐藏条件，我们就可以对请求按ClientId处理，哈希表每一个Client维护一个单调递增的序列号SequenceNum。在服务端只记录每个Client发送的请求中最后一条成功的请求。

这样判断重复的方式就比较简单了，将当前请求的SequenceNum与记录的SequenceNum相比，如果小于等于就直接返回之前的结果，否则继续。

问题1：当Server不是Leader时，是否应当将返回的结果存储到记录表中？

答：不应该，下表的情况可以说明为什么不应当这样做。
| 客户端1 | 服务端1 | 服务端2 |
|--|--|--|
| 创建 | 创建 | 创建 | 
| {命令1，序列号1}->服务端1 |  |  | 
|  | 记录{客户端1，序列号1,响应} 并返回 |  |
| {命令1，序列号1}->服务端2 | |  | 
|  | | 记录{客户端1，序列号1,响应} 并返回 |
|  | 选举成功，成为Leader |  |
| {命令1，序列号1}->服务端1 | |  | 
|  | 序列号1已被记录，直接响应 | |

问题2：日志中是否会存在重复的请求？

答：存在，考虑下面的情况。客户端发送给服务端1的请求，由于服务端1突然终止，但是日志已经被大部分服务器接收。因此这份请求就会保留在日志中，但还没有提交，接下来客户端发送给新的Leader，请求再次出现在日志中。因此日志中是可以有重复的请求的。对此，可以服务器对于下层raft提交的日志条目，可以从中取出ClientId与SequenceNum，然后进行重复请求判断。

| 客户端1 | 服务端1 | 服务端2 |
|--|--|--|
|  | 选举成功，成为Leader |  | 
| {命令1，序列号1}->服务端1 |  |  | 
|  | start{命令1，序列号1} |  |
| | 向服务端2发送{命令1，序列号1} |  | 
|  | | 接收{命令1，序列号1}，持久化 |
|  | 因为某种原因超时，向客户端1响应{ErrTimeout} |  |
|  |  | 选举成功，成为Leader |
| {命令1，序列号1}->服务端2 | |  | 
|  |  | start{命令1，序列号1} |
|  |  | 一系列同步操作后提交日志 |

难点：
本次实验的还有一个难点就在于报错的日志非常多，而且非常长。我的debug方法是通过从不一致第一次出现的地方开始进行搜索，发现。

在本次实验中，我使用channel作为唤醒协程的方式，然后没有注意到channel默认无缓冲的特性，导致出现了死锁。在此处介绍一个检查死锁的方式，对lock和unlock函数进行如下包装。
```go
func (kv *KVServer) lock(s string) {
	DPrintf(dServer, "KVS%d try lock during %s", kv.me, s)
	kv.mu.Lock()
	DPrintf(dServer, "KVS%d get lock during %s", kv.me, s)
}

func (kv *KVServer) unlock(s string) {
	DPrintf(dServer, "KVS%d try unlock during %s", kv.me, s)
	kv.mu.Unlock()
	DPrintf(dServer, "KVS%d complete unlock during %s", kv.me, s)
}
```

### 4A实验结果
1000次测试结果
```bash
$ VERBOSE=2 python3 dTest.py -p 50 -n 250 4A
┏━━━━━━┳━━━━━━━━┳━━━━━━━┳━━━━━━━━━━━━━━━┓
┃ Test ┃ Failed ┃ Total ┃          Time ┃
┡━━━━━━╇━━━━━━━━╇━━━━━━━╇━━━━━━━━━━━━━━━┩
│ 4A   │      0 │   250 │ 270.43 ± 2.62 │
└──────┴────────┴───────┴───────────────┘
```
单一运行时各个test的测试时间
```bash
$ go test -run 4A
Test: one client (4A) ...
  ... Passed --  15.0  5 41866 6850
Test: ops complete fast enough (4A) ...
  ... Passed --   1.1  3 21497    0
Test: many clients (4A) ...
  ... Passed --  15.1  5 65020 8785
Test: unreliable net, many clients (4A) ...
  ... Passed --  15.7  5  8245 1580
Test: concurrent append to same key, unreliable (4A) ...
  ... Passed --   1.0  3   207   52
Test: progress in majority (4A) ...
  ... Passed --   0.4  5    73    2
Test: no progress in minority (4A) ...
  ... Passed --   1.0  5   265    3
Test: completion after heal (4A) ...
  ... Passed --   1.0  5    92    3
Test: partitions, one client (4A) ...
  ... Passed --  22.4  5 35646 5656
Test: partitions, many clients (4A) ...
  ... Passed --  22.4  5 100789 8401
Test: restarts, one client (4A) ...
  ... Passed --  19.4  5 64880 6441
Test: restarts, many clients (4A) ...
  ... Passed --  20.6  5 139813 8506
Test: unreliable net, restarts, many clients (4A) ...
  ... Passed --  20.4  5  9291 1551
Test: restarts, partitions, many clients (4A) ...
  ... Passed --  26.9  5 182319 7861
Test: unreliable net, restarts, partitions, many clients (4A) ...
  ... Passed --  27.6  5  7902 1196
Test: unreliable net, restarts, partitions, random keys, many clients (4A) ...
  ... Passed --  29.1  7 18601 2762
PASS
ok      6.5840/kvraft   239.577s
```

## 4B: Key/value service with snapshots

实现一个带快照的容错的K/V服务。

### 实验难点分析

问题：状态机的哪些数据需要保存？
答：状态机本身的KV存储肯定是需要保存成为快照的，同时去重用的哈希Map也需要保存为快照，否则就会出现重复请求导致错误。

一个关于gob的知识点：如何对interface类型进行编程？使用register进行注册，同时对encode和decode进行包装，保证参数类型是interface。具体参考[GO GOB DOC](https://pkg.go.dev/encoding/gob#example-package-Interface)

在本次实验我修改了之前Server是否接收快照的判断语句，也就是InstallSnapShot的代码。具体可见如下注释。
```go
// 如果当前commitIndex大于等于LastIncludedIndex，则不做处理，因为未来Server自己把日志提交后就可以更新快照了。
// 或之后的条件是在Lab4b实现后新增，这是因为当前Server已经自己拍摄了一个快照
// 而Leader发送的快照太旧了，所以这里可以直接返回，等Leader的快照足够新后再接收快照
if args.LastIncludedIndex <= rf.CommitIndex || args.LastIncludedIndex <= rf.getFirstLog().Index{
  return
}
```

### 实验结果

```bash
VERBOSE=2 python3 dTest.py -p 50 -n 250  4B
┏━━━━━━┳━━━━━━━━┳━━━━━━━┳━━━━━━━━━━━━━━━┓
┃ Test ┃ Failed ┃ Total ┃          Time ┃
┡━━━━━━╇━━━━━━━━╇━━━━━━━╇━━━━━━━━━━━━━━━┩
│ 4B   │      0 │   250 │ 141.21 ± 1.68 │
└──────┴────────┴───────┴───────────────┘
```
单一运行时各个test的测试时间
```bash
$ go test -run 4B
Test: InstallSnapshot RPC (4B) ...
  ... Passed --   3.1  3 18128   63
Test: snapshot size is reasonable (4B) ...
  ... Passed --   0.4  3  9691  800
Test: ops complete fast enough (4B) ...
  ... Passed --   0.4  3 10854    0
Test: restarts, snapshots, one client (4B) ...
  ... Passed --  21.6  5 316822 56985
Test: restarts, snapshots, many clients (4B) ...
  ... Passed --  20.5  5 339882 76403
Test: unreliable net, snapshots, many clients (4B) ...
  ... Passed --  15.7  5  8443 1581
Test: unreliable net, restarts, snapshots, many clients (4B) ...
  ... Passed --  21.4  5  9470 1607
Test: unreliable net, restarts, partitions, snapshots, many clients (4B) ...
  ... Passed --  27.1  5  7540 1045
Test: unreliable net, restarts, partitions, snapshots, random keys, many clients (4B) ...
  ... Passed --  28.7  7 19008 2716
PASS
ok      6.5840/kvraft   138.860s
```

## 实验最终结果
目前存在的问题是，当worker数量选择的过多的时候，4A中的`Test: ops complete fast enough (4A)`速度测试无法通过。主要的问题是apply的太慢了。

```bash
$ python3 dTest.py -p 50 -n 500 4A 4B
┏━━━━━━┳━━━━━━━━┳━━━━━━━┳━━━━━━━━━━━━━━━┓
┃ Test ┃ Failed ┃ Total ┃          Time ┃
┡━━━━━━╇━━━━━━━━╇━━━━━━━╇━━━━━━━━━━━━━━━┩
│ 4A   │      0 │   500 │ 251.33 ± 2.30 │
│ 4B   │      0 │   500 │ 141.79 ± 1.36 │
└──────┴────────┴───────┴───────────────┘
```