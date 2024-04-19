package kvraft

import "fmt"

const (
	OK             = "OK"             //如果状态机应用了命令，则返回OK
	ErrNoKey       = "ErrNoKey"       //Get错误
	ErrWrongLeader = "ErrWrongLeader" //当前Sever不是Leader
	ErrTimeOut = "ErrTimeOut" //服务器超时（可能的原因：服务器当前被分区了）
)

type status string

type CommandOp string

const ( 
	PutOp CommandOp = "Put"
	AppendOp CommandOp = "Append"
	GetOp CommandOp = "Get"
)

type CommandArgs struct{
	Key         string
	Value       string
	Op          CommandOp // "Put" or "Append"
	ClientId int64 //客户端Id
	SequenceNum int64  // 用于去重
}

func (args *CommandArgs) String() string {
	return fmt.Sprintf("{K:%s,V:%s,Op:%v,CID:%d,SN:%d}",args.Key,args.Value,args.Op,args.ClientId,args.SequenceNum)
}

type CommandReply struct {
	Status     status //如果状态机应用了命令，则返回OK
	Response string // 如果状态OK，回复状态机的输出
}

func (reply *CommandReply) String() string {
	return fmt.Sprintf("{R:%s,S:%s}",reply.Response,reply.Status)
}