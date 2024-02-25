# 实验二：Key/Value Server

## 实验介绍

实验二要求为单机实现一个K/V服务器，该服务器可以保证即使出现网络故障，操作也可以执行一次，并且操作是按顺序的。在后面的实验中，将会使用这样的服务器来处理服务器崩溃。

client可以向K/V服务器发送三种不同的RPC：`Put(key,value)`，`Append(key,arg)`和`Get(key)`。服务器维护键值对的内存映射。key与value的类型都是字符串。`Put(key,value)`在K/V服务器的map中创建或更新内存中的特定键的值，`Append(key,arg)`将arg添加到键的值中然后返回旧值，`Get(key)`取回指定键的值。对于不存在的key，`Get`应当返回空字符串，`Append`行为如同`Put`。每个客户端都通过`Clerk`调用`Get`，`Append`和`Put`与服务器通信。`Clerk`管理与服务器的RPC交互。

对于应用程序对`Get`，`Append`和`Put`的调用，服务器必须安排某种顺序，使得调用顺序线性化。如果不是多个client的请求并发到达，那么每次`Get`，`Append`和`Put`调用应当遵守前面调用序列的修改。如果并发到达，返回值必须与操作按某种顺序执行那样得到的最终状态一样。（译者注：就是说在执行了两个写操作w1，w2后，用户接下来调用Get得到的值要么都是w1，要么都是w2）。所谓的并发到达即两次客户端的调用在时间上重叠。比如client X调用`Clerk.Put`，client Y调用`Clerk.Append`。任何一次调用开始之前都必须能观察到之前所有调用完成的效果。

线性化对于应用程序来说很有用，这可以让应用程序感觉服务器单独为他服务。比如，如果client从服务器中获得更新请求的成功响应，那么后续其他client的读取请求可以保证他们能够看到该更新的效果。对单个服务器来说，提供线性化相对容易。

## 实验要求
在`src/kvsrv`下编写代码。需要修改`kvsrv/client.go`,`kvsrc/server.go`,`kvsrc/common.go`三个文件。
### 无网络故障的K/V服务器
第一项任务时实现一个没有消息被丢弃的方案。
你需要将在`client.go`添加RPC发送方法，然后再`server.go`中实现Put/Append/Get处理程序。
使用go test进行，如果通过前两个测试说明该任务完成

### 存在网络故障的K/V服务器
现在你应当修改你的方法使得即使客户端与服务器端消息存在丢失，也可以保证正确运行。
如果消息丢失，那么client的`ck.server.Call`返回False（更准确地说，Call在一定时间内等待回复消息，如果最终超时，则返回false）。你面临的一个问题是，clerk可能会多次调用RPC直到成功。但是，每次`Clerk.Put`与`Clerk.Append`的调用应该只有一次执行，因此你必须确保重新发送不会导致服务器执行两次请求。

## 实验分析
对于实验要求1 无网络故障的K/V服务器
很简单，直接在server的结构体上加一个map结构，然后用一把锁保护它防止并发即可。
```go
type KVServer struct {
	mu sync.Mutex

	// Your definitions here.
	kvMp map[string]string //存储数据的map
}
```
以Get函数为例可以看到锁的使用
```go
func (kv *KVServer) Get(args *GetArgs, reply *GetReply) {
	// Your code here.
	kv.mu.Lock()
	defer kv.mu.Unlock()
	// do something
}
```

## 实验结果
### 实验要求1
很容易就可以通过
```bash
~/6.5840/src/kvsrv$ go test
Test: one client ...
  ... Passed -- t  4.1 nrpc 41423 ops 41423
Test: many clients ...
  ... Passed -- t  5.3 nrpc 144906 ops 144906
Test: unreliable net, many clients ...
--- FAIL: TestUnreliable2 (1.05s)
...
```
### 实验要求2