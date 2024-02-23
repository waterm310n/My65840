package mr

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io/fs"
	"io/ioutil"
	"log"
	"net/rpc"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Map函数返回[]KeyValue
type KeyValue struct {
	Key   string
	Value string
}

// for sorting by key.
type ByKey []KeyValue

// for sorting by key.
func (a ByKey) Len() int           { return len(a) }
func (a ByKey) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByKey) Less(i, j int) bool { return a[i].Key < a[j].Key }

// use ihash(key) % NReduce to choose the reduce
// task number for each KeyValue emitted by Map.
// 使用ihash(key)%NReduce为Map阶段释放的KeyValue，选择reduce的任务号
func ihash(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() & 0x7fffffff)
}

// main/mrwoker.go会调用该函数，其中参数是两个函数参数，分别是map函数与reduce函数
func Worker(mapf func(string, string) []KeyValue,
	reducef func(string, []string) string) {
	// Your worker implementation here.
	message := Response{}
	ok := call("Coordinator.GetTask", 0, &message)
	for ok {
		switch message.TaskType {
		case MAP:
			Map(mapf, &message)
		case REDUCE:
			Reduce(reducef, &message)
		case SLEEP:
			log.Println("worker: nothing to do temporarily")
			time.Sleep(2 * time.Second)
		case FINISH:
			log.Println("worker: worker died")
			return
		}
		message = Response{}
		ok = call("Coordinator.GetTask", 0, &message)
	}
}

// 判断文件或目录是否存在
func checkFileOrDirectoryExist(path string) bool {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false
	}
	return true
}

// 生成写json文件的Handle
func createWriteFileHandle(taskNum int, NReduce int) []*json.Encoder {
	res := make([]*json.Encoder, NReduce)
	return res
}

// 生成读json文件的Handle
func createReadFileHandle(taskNum int) []*json.Decoder {
	res := make([]*json.Decoder, 0)
	err := filepath.Walk("intermediate", func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			log.Fatalf("prevent panic by handling failure accessing a path %q: %v\n", path, err)
			return err
		}
		fileName := info.Name()
		parts := strings.Split(fileName, "-")
		num, _ := strconv.Atoi(parts[len(parts)-1])
		if num == taskNum {
			file, err := os.Open(path)
			if err != nil {
				log.Fatalf("cannot open file %s,error %s", path, err.Error())
			}
			res = append(res, json.NewDecoder(file))
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	return res
}

// Map阶段
func Map(mapf func(string, string) []KeyValue, msg *Response) error {
	intermediate := []KeyValue{}
	for _, filename := range msg.Files {
		file, err := os.Open(filename)
		if err != nil {
			log.Fatalf("cannot open %v", filename)
		}
		content, err := ioutil.ReadAll(file)
		if err != nil {
			log.Fatalf("cannot read %v", filename)
		}
		file.Close()
		kva := mapf(filename, string(content))
		intermediate = append(intermediate, kva...)
	}
	if !checkFileOrDirectoryExist("intermediate") {
		if err := os.Mkdir("intermediate", 0755); err != nil {
			log.Fatal(err)
			panic("can not mkdir intermediate")
		}
	}
	nReduce := msg.NReduce
	handles := createWriteFileHandle(msg.TaskNum, nReduce)
	//将结果以json的形式写入文件中
	for _, v := range intermediate {
		reduceNum := ihash(v.Key) % nReduce
		if handles[reduceNum] == nil {
			file, err := os.Create(filepath.Join("intermediate", fmt.Sprintf("mr-%d-%d", msg.TaskNum, reduceNum)))
			if err != nil {
				log.Fatal(err)
			}
			handles[reduceNum] = json.NewEncoder(file)
		}
		handles[reduceNum].Encode(&v)
	}
	request := Request{
		TaskNum:  msg.TaskNum,
		TaskType: msg.TaskType,
	}
	if ok := call("Coordinator.CompleteTask", request, nil); ok {
		log.Printf("worker: finished map task %d", msg.TaskNum)
	}
	return nil
}

// Reduce阶段
func Reduce(reducef func(string, []string) string, msg *Response) error {
	if !checkFileOrDirectoryExist("intermediate") {
		if err := os.Mkdir("intermediate", 0755); err != nil {
			log.Fatal(err)
			panic("intermediate directory is not exsist")
		}
	}
	handles := createReadFileHandle(msg.TaskNum)
	ofile, err := os.Create(fmt.Sprintf("mr-out-%d", msg.TaskNum))
	if err != nil {
		log.Fatal(err)
	}
	kva := []KeyValue{}
	for _, handle := range handles {
		for {
			var kv KeyValue
			if err := handle.Decode(&kv); err != nil {
				break
			}
			kva = append(kva, kv)
		}
	}
	sort.Sort(ByKey(kva))
	i := 0
	for i < len(kva) {
		j := i + 1
		for j < len(kva) && kva[j].Key == kva[i].Key {
			j++
		}
		values := []string{}
		for k := i; k < j; k++ {
			values = append(values, kva[k].Value)
		}
		output := reducef(kva[i].Key, values)
		// this is the correct format for each line of Reduce output.
		fmt.Fprintf(ofile, "%v %v\n", kva[i].Key, output)
		i = j
	}
	request := Request{
		TaskNum:  msg.TaskNum,
		TaskType: msg.TaskType,
	}
	if ok := call("Coordinator.CompleteTask", request, nil); ok {
		log.Printf("worker: finished reduce task %d", msg.TaskNum)
	}
	return nil
}

// 向coordinator发送rpc请求，等待回复，函数的返回值通常为true
// 返回false意味出错
func call(rpcname string, args interface{}, reply interface{}) bool {
	// c, err := rpc.DialHTTP("tcp", "127.0.0.1"+":1234")
	sockname := coordinatorSock()
	c, err := rpc.DialHTTP("unix", sockname)
	if err != nil {
		log.Fatal("dialing:", err)
	}
	defer c.Close()

	err = c.Call(rpcname, args, reply)
	if err == nil {
		return true
	}

	fmt.Println(err)
	return false
}
