package kvraft

import (
	"crypto/rand"
	"math/big"


	"6.5840/labrpc"
)

// 此处的Clerk执行的一次只能执行一条命令，因此没有区分Update与Get。
type Clerk struct {
	servers []*labrpc.ClientEnd
	// You will have to modify this struct.
	clienId           int64 //当前客户端的ID
	commandSequnceNum int64 //命令的序列号,单调递增
	curLeader         int   // 当前可能的Leader,初始化为0
}

// 创建客户端
func MakeClerk(servers []*labrpc.ClientEnd) *Clerk {
	ck := &Clerk{
		servers:   servers,
		clienId:   nrand(),
		curLeader: 0,
		commandSequnceNum:0,
	}
	DPrintf(dClient,"C%d create",ck.clienId)
	return ck
}

// 随机数生成
func nrand() int64 {
	max := big.NewInt(int64(1) << 62)
	bigx, _ := rand.Int(rand.Reader, max)
	x := bigx.Int64()
	return x
}

// 根据Key获取Value
func (ck *Clerk) Get(key string) string {
	return ck.Command(&CommandArgs{Key: key, Op: GetOp,})
}

func (ck *Clerk) Put(key string, value string) {
	ck.Command(&CommandArgs{Key: key, Value: value, Op: PutOp})
}
func (ck *Clerk) Append(key string, value string) {
	ck.Command(&CommandArgs{Key: key, Value: value, Op: AppendOp})
}

func (ck *Clerk) Command(args *CommandArgs) string {
	ck.commandSequnceNum += 1
	args.ClientId,args.SequenceNum = ck.clienId,ck.commandSequnceNum
	ok := false
	for {
		reply := &CommandReply{}
		DPrintf(dClient,"C%d request %v",ck.clienId,args)
		ok = ck.servers[ck.curLeader].Call("KVServer.Command",args,reply)
		if !ok || reply.Status == ErrWrongLeader || reply.Status == ErrTimeOut {
			ck.curLeader = (ck.curLeader+1)%len(ck.servers) //循环遍历，直到找到可用的服务器
			continue
		}
		DPrintf(dClient,"C%d request %v get Response %v",ck.clienId,args,reply)
		return reply.Response
	}
}
