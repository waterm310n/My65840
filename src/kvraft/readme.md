# 测试说明

在前面的Test中，对KV状态机的KV改变的手法如下。

每一个C，首先Put 一个 空 Value。然后不停地Append x {0..nClient} num y 其中num单调递增