package kvsrv

import (
	"log"
	"sync"
)

const Debug = false

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug {
		log.Printf(format, a...)
	}
	return
}

type KVServer struct {
	mu sync.Mutex

	// Your definitions here.
	kvMp map[string]string //存储数据的map
	idMp map[int64]string  //键为每次操作的id,O(1)插入,O(1)删除,值为每次操作后的缓存值
}

func (kv *KVServer) Get(args *GetArgs, reply *GetReply) {
	// Your code here.
	kv.mu.Lock()
	defer kv.mu.Unlock()
	if value, ok := kv.idMp[args.Id]; ok {
		reply.Value = value
		return
	}
	if value, ok := kv.kvMp[args.Key]; ok {
		reply.Value = value
		kv.idMp[args.Id] = value
	}
}

func (kv *KVServer) Put(args *PutAppendArgs, reply *PutAppendReply) {
	// Your code here.
	kv.mu.Lock()
	defer kv.mu.Unlock()
	if _, ok := kv.idMp[args.Id]; ok {
		return
	}
	kv.kvMp[args.Key] = args.Value
	kv.idMp[args.Id] = args.Value
}

func (kv *KVServer) Append(args *PutAppendArgs, reply *PutAppendReply) {
	// Your code here.
	kv.mu.Lock()
	defer kv.mu.Unlock()
	if value, ok := kv.idMp[args.Id]; ok {
		reply.Value = value
		return
	}
	oldValue := kv.kvMp[args.Key]
	kv.kvMp[args.Key] = oldValue + args.Value
	reply.Value = oldValue
	kv.idMp[args.Id] = oldValue
}

func (kv *KVServer) DeleteId(args *DeleteArgs, reply *DeleteReply) {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	delete(kv.idMp, args.Id)
}

func StartKVServer() *KVServer {
	kv := new(KVServer)

	// You may need initialization code here.
	kv.kvMp = make(map[string]string)
	kv.idMp = make(map[int64]string)
	return kv
}
