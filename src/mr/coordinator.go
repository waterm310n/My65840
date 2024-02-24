package mr

import (
	"context"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"os"
	"sync"
	"time"
)

type TaskState int

// 任务状态
const (
	IDLE     = iota //空闲状态
	WORK            //工作状态
	COMPLETE        //完成状态
)

type TaskType int

// 任务类型
const (
	MAP    = iota //MAP任务
	REDUCE        //REDUCE任务
	SLEEP         //没有任务可做，请休息
	FINISH
)

type Task struct {
	fileName  string    //任务对应的文件名称
	taskState TaskState //任务状态
	taskNum   int       //任务编号
	taskType  TaskType  //任务类型
	cancel    context.CancelFunc
}

type Coordinator struct {
	// Your definitions here.
	//任务队列，先进先出
	mutex                 sync.Mutex //锁
	curPtrTask            int        //当前处在working状态的任务
	mapTaskQueue          []*Task    //先进先出的单向循环map任务队列
	totalMapTask          int        //总共需要完成的map任务数
	reduceTaskQueue       []*Task    //先进先出的单向循环reduce任务队列
	totalReduceTask       int        //总共需要完成的reduce任务数
	remainMapTaskCount    int        //未完成的Map任务数
	remainReduceTaskCount int        //未完成的Reduce任务数
	nReduce               int        //nReduce
}

// 寻找下一个空闲的Map任务,如果没有则返回nil
func (c *Coordinator) findNextIdleMapTask() *Task {
	temp := c.curPtrTask
	if c.mapTaskQueue[temp].taskState != IDLE {
		temp = (temp + 1) % c.totalMapTask
	} else {
		c.curPtrTask = temp
		return c.mapTaskQueue[temp]
	}
	for temp != c.curPtrTask {
		//找一圈看看有没有Idle Task
		if c.mapTaskQueue[temp].taskState == IDLE {
			c.curPtrTask = temp
			return c.mapTaskQueue[temp]
		}
		temp = (temp + 1) % c.totalMapTask
	}
	return nil
}

// 寻找下一个空闲的Reduce任务,如果没有则返回nil
func (c *Coordinator) findNextIdleReduceTask() *Task {
	temp := c.curPtrTask
	if c.reduceTaskQueue[temp].taskState != IDLE {
		temp = (temp + 1) % c.totalReduceTask
	} else {
		c.curPtrTask = temp
		return c.reduceTaskQueue[temp]
	}
	for temp != c.curPtrTask {
		//找一圈看看有没有Idle Task
		if c.reduceTaskQueue[temp].taskState == IDLE {
			c.curPtrTask = temp
			return c.reduceTaskQueue[temp]
		}
		temp = (temp + 1) % c.totalReduceTask
	}
	return nil
}

// Your code here -- RPC handlers for the worker to call.
// 请求任务
func (c *Coordinator) GetTask(request int, response *Response) error {
	c.mutex.Lock()
	if c.remainMapTaskCount != 0 {
		//还有未完成的map任务
		task := c.findNextIdleMapTask()
		if task == nil {
			//当前没有处在IDLE状态的任务，此时通知Worker进行休息，即sleep
			response.TaskType = SLEEP
			//提前退出记得释放锁
			c.mutex.Unlock()
			return nil
		}
		response.Files = append(response.Files, task.fileName)
		response.NReduce = c.nReduce
		response.TaskNum = task.taskNum
		response.TaskType = task.taskType
		//十秒钟的任务超时时间
		ctx, cancel := context.WithTimeout(context.TODO(), time.Second*10)
		task.taskState = WORK
		task.cancel = cancel
		go func(ctx context.Context) {
			<-ctx.Done()
			c.mutex.Lock()
			if task.taskState != COMPLETE {
				task.taskState = IDLE
				log.Printf("coordinator : map task %s timeout", task.fileName)
			}
			c.mutex.Unlock()
		}(ctx)
	} else if c.remainMapTaskCount == 0 && c.remainReduceTaskCount != 0 {
		//还有未完成的reduce任务 TODO
		task := c.findNextIdleReduceTask()
		if task == nil {
			response.TaskType = SLEEP
			// 提前退出释放锁
			c.mutex.Unlock()
			return nil
		}
		response.NReduce = c.nReduce
		response.TaskNum = task.taskNum
		response.TaskType = task.taskType
		ctx, cancel := context.WithTimeout(context.TODO(), time.Second*10)
		task.taskState = WORK
		task.cancel = cancel
		go func(ctx context.Context) {
			<-ctx.Done()
			c.mutex.Lock()
			if task.taskState != COMPLETE {
				task.taskState = IDLE
				log.Printf("reduce task %s timeout", task.fileName)
			}
			c.mutex.Unlock()
		}(ctx)
	} else {
		//所有任务都完成了,此时再有worker想干活就告诉他们没活干了
		response.TaskType = FINISH
	}
	c.mutex.Unlock()
	return nil
}

// 完成任务
func (c *Coordinator) CompleteTask(request Request, message *Response) error {
	c.mutex.Lock()
	if request.TaskType == MAP {
		//完成Map任务
		c.mapTaskQueue[request.TaskNum].taskState = COMPLETE
		c.mapTaskQueue[request.TaskNum].cancel()
		log.Printf("coordinator: remainMapTask %d to finish", c.remainMapTaskCount)
		c.remainMapTaskCount--
		if c.remainMapTaskCount == 0 {
			//完成所有Map任务
			log.Print("coordinator: all map tasks finished")
			c.curPtrTask = 0
		}
	} else {
		//完成Reduce任务
		c.reduceTaskQueue[request.TaskNum].taskState = COMPLETE
		c.reduceTaskQueue[request.TaskNum].cancel()
		log.Printf("coordinator: remainReduceTask %d to finish", c.remainReduceTaskCount)
		c.remainReduceTaskCount--
		if c.remainReduceTaskCount == 0 {
			//完成所有Reduce任务
			log.Print("coordinator: all reduce tasks finished")
		}
	}
	c.mutex.Unlock()
	return nil
}

// 开启线程监听来自worker.go的RPCs
func (c *Coordinator) server() {
	rpc.Register(c)
	rpc.HandleHTTP()
	sockname := coordinatorSock()
	os.Remove(sockname)
	// 使用unix作为传输层
	l, e := net.Listen("unix", sockname)
	if e != nil {
		log.Fatal("listen error:", e)
	}
	go http.Serve(l, nil)
}

// main/mrcoordinator.go会无限循环调用本函数
// 以判断mapreduce job是否完成
func (c *Coordinator) Done() bool {
	c.mutex.Lock()
	ret := false
	// Your code here.
	if c.remainMapTaskCount == 0 && c.remainReduceTaskCount == 0 {
		return true
	}
	c.mutex.Unlock()
	return ret
}

// 创建Coordinator，main/mrcoordinator.go将调用本函数
// nReduce是要分配的reduce任务的数量
func MakeCoordinator(files []string, nReduce int) *Coordinator {
	c := Coordinator{}
	// Your code here.
	// TODO 可以对文件进行划分
	c.mapTaskQueue = []*Task{}
	for i, fileName := range files {
		c.mapTaskQueue = append(c.mapTaskQueue, &Task{fileName: fileName, taskState: IDLE, taskType: MAP, taskNum: i})
	}
	c.totalMapTask = len(c.mapTaskQueue)
	//我的理解是nReduce是多少就代表有多少个输出文件，因此只需要最多nReduce个worker执行程序
	for i := 0; i < nReduce; i++ {
		c.reduceTaskQueue = append(c.reduceTaskQueue, &Task{taskState: IDLE, taskType: REDUCE, taskNum: i})
	}
	c.totalReduceTask = nReduce
	c.remainMapTaskCount = c.totalMapTask
	c.remainReduceTaskCount = nReduce
	c.curPtrTask = 0
	c.nReduce = nReduce
	c.server()
	return &c
}
