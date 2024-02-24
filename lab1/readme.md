# 实验一：MapReduce

## 实验要求

实现一个分布式的MapReduce，它由两个程序组成，coordinator与worker。将只有一个coordinator进程和一个或多个并行执行的worker进程。在真实世界中，worker会在一组不同的机器上运行，但是在这个实验中，只需在一台机器上运行。worker进程通过RPC与coordinator进程通信。在一次循环中，每个worker进程都会向coordinator请求一个任务，从一个或多个文件中读取任务的输入，接着执行任务，最后将任务的输出写入一个或多个文件，然后再次向coordinator请求一个新任务。coordinator应该注意如果一个worker进程在合理的时间内没有完成任务(对于本次实验，时间为10秒钟) ，应当将相同的任务交给另一个worker。

coordinator和 worker 的main函数位于 main/mrCollaborator.go 和 main/mrworker.go 中，但不要更改这些文件。
在`mr/coordinator.go`, `mr/worker.go`,和`mr/rpc.go`中完成代码实现.

## 实验守则
1. Map 阶段应该将中间结果划分到桶中，对应 `nReduce`个`reduce`任务 ，`nReduce` 是 `reduce` 任务的数量——在 `main/mrcoordinator.go`文件中`mr.MakeCoordinator(os.Args[1:], 10)`的第二个参数为`nReduce`，默认值是10。每个Map应该创建 `nReduce`个中间文件，以供`reduce`阶段使用。

2. `worker`实现中，第X个`reduce`任务的输出文件命名为`mr-out-X`

3. `mr-out-X`文件应该包含`Reduce`函数的每一行输出。并且每一行都应该组织成`%v %v`的形式，其中第一个值表示Key，第二个值表示Value。可以在`main/mrsequential.go`文件中的注释`this is the correct format`下找到格式化语句。（即：`fmt.Fprintf(ofile, "%v %v\n", intermediate[i].Key, output)`）如果实现与这种格式差距过大，测试脚本无法完成测试。

4. 你可以修改`mr/coordinator.go`, `mr/worker.go`,和`mr/rpc.go`文件。你可以为了测试暂时修改其他文件，但请确保你的代码能够与原始版本一起工作时可以正常运行。
   
5. `worker`应当将中间的Map结果放置到当前目录中，之后`reduce`任务可以将这些文件作为输入。
   
6. 当你正确实现MapReduce实验时，`mr/coordinator.go`的`Done()`方法应该返回true。这样`main/mrcoordinator.go`才会结束运行。
   
7. 当job完全完成时，worker进程应当退出。实现这种方式的一种简单方法是使用`call()`函数的返回值：如果worker无法连接coordinator，它可以确保当coordinate因为job完成退出时，worker进程也可以结束。这取决于你的实现，你也许还会发现有一个“请退出”伪任务会很有用，coordinator可以将这个伪任务分配给worker以结束进程。

## 实验分析
首先确立debug环境，使用下面命令生成wc.so,不过对于为什么需要`-gcflags="all=-N -l"`（不使用该flag，则使用vscode调试时，会提示无法加载plugin）
```bash
go build -buildmode=plugin -gcflags="all=-N -l" ../mrapps/wc.go
```

下面分析coordinator和worker的功能：

worker的功能有：
- 向coordinator请求任务
- 根据coordinator任务类型，执行特定Map任务或Reduce任务
- 完成任务后，通知coordinar任务完成

注意到`test-mr.bash`在运行的时候，会启动固定数量的worker。因此每个worker中应该启动一个循环，每次执行完任务后，询问coordinator是否有任务可做，暂时没有任务就先sleep，如果所有任务都做完了才结束程序。

coordinator的功能有：
- 当worker请求任务时，向worker发布任务，同时应该告知worker任务的类型是什么，任务的内容，任务编号。
- 当worker任务完成时，coordinator接收到worker任务完成的信息，并从中根据任务编号得知哪项任务完成，将对应的任务标为完成。
- 记录需要完成任务数量，每有一项任务完成就减一，当需要完成的任务数量为0时，结束程序。

注意到，在mapreduce程序中，只有所有的map任务都完成的时候，reduce任务才可以开始执行。因此我这里使用两个变量，分别记录剩余未完成的map任务，剩余未完成的reduce任务。

### 功能实现说明
首先考虑worker与coordinator之间的通信，相关的通信函数实验已经给出
```go
func call(rpcname string, args interface{}, reply interface{}) bool
```
因此只需定义两者之间的通信格式，对此我定义如下
```go
type Request struct {
	TaskNum  int      //如果完成任务，那么是任务编号
	TaskType TaskType //如果完成任务，那么是任务类型
}

type Response struct {
	Files    []string //在Map中使用到，Map任务要读取的文件名
	NReduce  int    //总共的reduce任务分类
	TaskNum  int      //任务编号
	TaskType TaskType //任务类型
}
```

为了在coordinator上实现任务管理，并且一旦任务超时，进行重新分配。我选择使用队列作为数据结构。从下面的代码可以看出，我并没有实际出队，而是循环一圈遍历队列，判断任务状态是否是空闲的，如果是空闲的则返回；否则返回nil
```go
//下面是简化代码
type TaskQueue []*Task

func findNextTask(ptrTask int) *Task {
	temp := ptrTask
	if TaskQueue[temp].taskState != IDLE {
		temp = (temp + 1) % totalTask
	} else {
		return TaskQueue[temp]
	}
	for temp != ptrTask {
		//找一圈看看有没有空闲的任务
		if TaskQueue[temp].taskState == IDLE {
			return TaskQueue[temp]
		}
		temp = (temp + 1) % totalTask
	}
	return nil
}
```
判断任务超时，此处使用context，如果超时则重新设置任务状态为空闲
## 实验结果
已经通过的test
```bash
bash test-mr.sh quiet #忽视coordinator与worker的输出

*** Starting wc test.
--- wc test: PASS
*** Starting indexer test.
--- indexer test: PASS
*** Starting map parallelism test.
--- map parallelism test: PASS
*** Starting reduce parallelism test.
--- reduce parallelism test: PASS
*** Starting job count test.
--- map jobs ran incorrect number of times (8
8
...
8 != 8)
--- job count test: FAIL
*** Starting early exit test.
--- output changed after first worker exited
--- early exit test: FAIL
*** Starting crash test.
mr-crash-all mr-correct-crash.txt differ: byte 1, line 1
--- crash output is not the same as mr-correct-crash.txt
--- crash test: FAIL
*** FAILED SOME TESTS
```
能够看到出错的位置在jobCount，于是我手动执行了一遍，我发现理论上是没有问题的，但是我的输出打印了很多遍8。因此我仔细阅读了test-mr.sh中关于jobCount位置的测试代码。代码如下
```bash
echo '***' Starting job count test.
# 删除输出文件
rm -f mr-*
# 执行coordinator
maybe_quiet $TIMEOUT ../mrcoordinator ../pg*txt  &
sleep 1
# 运行worker
maybe_quiet $TIMEOUT ../mrworker ../../mrapps/jobcount.so &
maybe_quiet $TIMEOUT ../mrworker ../../mrapps/jobcount.so
maybe_quiet $TIMEOUT ../mrworker ../../mrapps/jobcount.so &
maybe_quiet $TIMEOUT ../mrworker ../../mrapps/jobcount.so
# 判断结果是否为8
NT=`cat mr-out* | awk '{print $2}'`
if [ "$NT" -eq "8" ]
then
  echo '---' job count test: PASS
else
  echo '---' map jobs ran incorrect number of times "($NT != 8)"
  echo '---' job count test: FAIL
  failed_any=1
fi

wait
```
因此可知，测试脚本只会主动清理输出文件。我首次编写的程序产生的副作用代码需要自己管理，而我的reduce任务分配处理方式是直接遍历reduce，也就是说即使有reduce没必要处理我也会读取，这导致我会读取到前次任务的结果导致出错。因此我修改了coordinator与map之间的策略。现在request还会带上有哪些reduce任务需要完成。修改后成功通过测试,不过有unexpected EOF错误，可能是打印操作造成的，如果有空再回来修复这个问题。
```bash
bash test-mr.sh quiet #忽视coordinator与worker的输出

*** Starting wc test.
--- wc test: PASS
*** Starting indexer test.
--- indexer test: PASS
*** Starting map parallelism test.
--- map parallelism test: PASS
*** Starting reduce parallelism test.
--- reduce parallelism test: PASS
*** Starting job count test.
--- job count test: PASS
*** Starting early exit test.
--- early exit test: PASS
*** Starting crash test.
unexpected EOF
--- crash test: PASS
*** PASSED ALL TESTS
```
我又额外跑了10次测试脚本，全部测试都通过了
```bash
bash test-mr-many.sh 10

*** PASSED ALL TESTS
*** PASSED ALL 10 TESTING TRIALS
```