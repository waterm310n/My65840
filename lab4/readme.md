# 实验三：容错的K/V服务

客户端可以使用下面三种RPC

`Put(key,value)`:替换数据库中特定键的值
`Append(key,value)`:将arg追加到key的值(如果键不存在，则将现有值视为空字符串)
`Get(key)`:获取当前键的值(对于不存在的键返回空字符串)

键和值都是字符串。请注意，与实验 2 不同， Put 与 Append 不应向客户端返回值。

需要修改`src/kvraft`中的`kvraft/client.go`,`kvraft/server.go`,`kvraft/common.go`