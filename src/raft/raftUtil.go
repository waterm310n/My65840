package raft

//
// 为raft实现提供需要的工具支持，为了方便管理，将这部分从raft.go中移出
//

import (
	"math/rand"
	"time"
)

const HEARTBEATETIME = time.Duration(60) * time.Millisecond //心跳发送时间间隔

// 随机的选举时间
func randomizedElectionTimeout() time.Duration {
	ms := 240 + (rand.Int63() % 720)
	return time.Duration(ms) * time.Millisecond
}

type RaftState int // Raft节点的状态类型，分为Leader,Candidate,Follower

const (
	LEADER RaftState = iota
	CANDIDATE
	FOLLOWER
)

type BroadcastType int

const (
	HEARTBEAT BroadcastType = iota //心跳类型的广播
	REPLICATE                      //复制类型的广播
)

// 日志条目
type LogEntry struct {
	Term    int         //日志条目的任期
	Command interface{} //日志条目的命令
	Index   int         // 日志条目所对应的下标，需要记录这个原因是3D应用快照的时候，log对应的下标不再是CommandIndex
}

// GC友好函数
func shrinkEntriesArray(log []LogEntry) []LogEntry {
	return append([]LogEntry{}, log...)
}

func maxInt(a int, nums ...int) int {
	maxNum := a
	for _, num := range nums {
		if num > maxNum {
			maxNum = num
		}
	}
	return maxNum
}

func minInt(a int, nums ...int) int {
	minNum := a
	for _, num := range nums {
		if num < minNum {
			minNum = num
		}
	}
	return minNum
}
