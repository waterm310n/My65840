package mr

// RPC 定义
// 记得将所有Name首字母大写以导出

import (
	"os"
	"strconv"
)

// 在此处添加你的RPC定义
type Request struct {
	TaskNum  int      //如果完成任务，那么是任务编号
	TaskType TaskType //如果完成任务，那么是任务类型
}

type Response struct {
	Files    []string
	NReduce  int
	TaskNum  int      //任务编号
	TaskType TaskType //任务类型
}

type RequestType int

// 在/var/tmp为coordinator创建一个唯一的UNIX域套接字名称
// 因为Athena AFS不支持UNIX域套接字，所以不能使用当前目录（看不懂这句话想表达的意思，Athena好像是MIT的一个教学系统？）
func coordinatorSock() string {
	s := "/var/tmp/5840-mr-"
	s += strconv.Itoa(os.Getuid())
	return s
}
