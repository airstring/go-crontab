package master

import (
	"crontab/common"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"
)

//任务的http接口
type ApiServer struct {
	httpServer *http.Server
}

var (
	//单例对象
	G_apiServer *ApiServer
)

//保存任务接口
//POST job={"name": "job1", "command": "echo hello", "cronExpr": "* * * * *"}
func handleJobSave(resp http.ResponseWriter, req *http.Request) {
	fmt.Println("handleJobSave")
	var (
		postJob string
		job     common.Job
		oldJob  *common.Job
		bytes   []byte
		err     error
	)
	//任务保存到etcd中
	//1.解析post表单
	//golanb的http服务端不会主动解析，这个解析需要耗费cpu的，所以需要主动调用它的一个方法去解析
	if err = req.ParseForm(); err != nil {
		fmt.Println("req.parseform err")
		goto ERR
	}
	//2.获取表单中的job字段
	postJob = req.PostForm.Get("job")
	fmt.Println(postJob) //{"name":"job1", "command":"echo hello","cronExpr":"*/5 * * * * * *"}
	//3.反序列化job
	if err := json.Unmarshal([]byte(postJob), &job); err != nil {
		fmt.Println("job err")
		goto ERR
	}

	//4.保存到etcd
	if oldJob, err = G_jobMgr.SaveJob(&job); err != nil {
		goto ERR
	}

	//5.返回正常应答（{"errno: 0, "msg": "", "data": {...}}）
	if bytes, err = common.BuildResponse(0, "success", oldJob); err == nil {
		fmt.Println("befor response")
		resp.Write(bytes)
	}
	return
ERR:
	//6.返回异常应答
	if bytes, err = common.BuildResponse(-1, err.Error(), nil); err == nil {
		resp.Write(bytes)
	}
}

//删除任务接口
//POST /job/delete name=job1 提交这么一个表单
func handleJobDelete(resp http.ResponseWriter, req *http.Request) {
	var (
		err    error
		name   string
		oldJob *common.Job
		bytes  []byte
	)
	//我们提交的一般是 a=1&b=2&c=3，这里的解析做了一个切割
	if err = req.ParseForm(); err != nil {
		goto ERR
	}
	//删除的任务名字
	name = req.PostForm.Get("name")

	//去删除任务
	if oldJob, err = G_jobMgr.DeleteJob(name); err != nil {
		goto ERR
	}
	//正常应答
	if bytes, err = common.BuildResponse(0, "success", oldJob); err == nil {
		resp.Write(bytes)
	}
	return
ERR:
	if bytes, err = common.BuildResponse(-1, err.Error(), nil); err == nil {
		resp.Write(bytes)
	}
}

//列举所有的crontab任务，这里没有实现翻页,一次性拿出所有的任务
func handleJobList(resp http.ResponseWriter, req *http.Request) {
	var (
		jobList []*common.Job
		err     error
		bytes   []byte //是一个序列化后应答的json字符串,byte类型的数组

	)
	//获取任务列表
	if jobList, err = G_jobMgr.ListJobs(); err != nil {
		goto ERR
	}

	//返回正常应答
	if bytes, err = common.BuildResponse(0, "success", jobList); err == nil {
		resp.Write(bytes)
	}
	return
ERR:
	if bytes, err = common.BuildResponse(-1, err.Error(), nil); err == nil {
		resp.Write(bytes)
	}

}

//杀死任务
//POST /job/kill name=job1
func handleJobKill(resp http.ResponseWriter, req *http.Request) {
	var (
		err   error
		name  string
		bytes []byte
	)
	//解析post表单
	if err = req.ParseForm(); err != nil {
		goto ERR
	}
	//要杀死的任务名
	name = req.PostForm.Get("name")
	//杀死任务
	if err = G_jobMgr.KillJob(name); err != nil {
		goto ERR
	}
	//正常应答
	if bytes, err = common.BuildResponse(0, "success", nil); err == nil {
		resp.Write(bytes)
	}
	return
ERR:
	if bytes, err = common.BuildResponse(-1, err.Error(), nil); err == nil {
		resp.Write(bytes)
	}
	//老师的这里没有return语句
}

//查询任务日志
func handleJobLog(resp http.ResponseWriter, req *http.Request) {
	var (
		err       error
		name      string //任务名字
		skipParm  string //从第几条开始 翻页参数
		limitParm string //限制返回多少条
		skip      int
		limit     int
		logArr    []*common.JobLog
		bytes     []byte
	)
	//解析GET参数，golang默认是不解析的。
	if err = req.ParseForm(); err != nil {
		goto ERR
	}
	//获取请求参数，/job/log?name=job10&skip=0&limit=10
	name = req.Form.Get("name")
	fmt.Println(name)
	skipParm = req.Form.Get("skip")
	limitParm = req.Form.Get("limit")
	//将字符串转换成整型
	if skip, err = strconv.Atoi(skipParm); err != nil {
		skip = 0
	}
	if limit, err = strconv.Atoi(limitParm); err != nil {
		limit = 20
	}
	if logArr, err = G_logMgr.ListLog(name, skip, limit); err != nil {
		goto ERR
	}

	//正常应答
	if bytes, err = common.BuildResponse(0, "success", logArr); err == nil {
		resp.Write(bytes)
	}
	return
ERR:
	if bytes, err = common.BuildResponse(-1, err.Error(), nil); err == nil {
		resp.Write(bytes)
	}
	//老师的这里没有return语句
}

//获取健康worker节点列表
func handleWorkerList(resp http.ResponseWriter, req *http.Request) {
	var (
		workerArr []string
		err       error
		bytes     []byte
	)
	if workerArr, err = G_workerMgr.ListWorkers(); err != nil {
		goto ERR
	}
	//正常应答
	if bytes, err = common.BuildResponse(0, "success", workerArr); err == nil {
		resp.Write(bytes)
	}
	return
ERR:
	if bytes, err = common.BuildResponse(-1, err.Error(), nil); err == nil {
		resp.Write(bytes)
	}
	//老师的这里没有return语句
}

//初始化服务
func InitApiServer() (err error) {
	var (
		staticDir     http.Dir
		staticHandler http.Handler
	)
	mux := http.NewServeMux()                      //创建路由对象
	mux.HandleFunc("/job/save", handleJobSave)     //配置路由 保存任务
	mux.HandleFunc("/job/delete", handleJobDelete) //删除任务
	mux.HandleFunc("/job/list", handleJobList)     //列举当前有哪些任务
	mux.HandleFunc("/job/kill", handleJobKill)
	mux.HandleFunc("/job/log", handleJobLog)
	mux.HandleFunc("/worker/list", handleWorkerList)

	//静态文件根目录
	staticDir = http.Dir(G_config.WebRoot)
	//静态文件的http回调
	staticHandler = http.FileServer(staticDir)
	mux.Handle("/", http.StripPrefix("/", staticHandler)) //  /index.html会交给http.StripPrefix，被去掉前缀/后再将index.html交给staticHandler，再找./webroot/index.html
	//mux.Handle("/index", http.StripPrefix("/", staticHandler))
	//启动tcp监听
	listener, err := net.Listen("tcp", "0.0.0.0:"+strconv.Itoa(G_config.ApiPort))
	if err != nil {
		return err
	}
	//创建一个http服务
	httpServer := &http.Server{
		ReadTimeout:  time.Duration(G_config.ApiReadTimeout) * time.Millisecond,
		WriteTimeout: time.Duration(G_config.ApiWriteTimeout) * time.Millisecond,
		Handler:      mux,
	}
	//赋值单例
	G_apiServer = &ApiServer{ //需要被其他的包访问到，所以首字母需要大写
		httpServer: httpServer,
	}
	//启动了服务端

	go httpServer.Serve(listener)

	return
}
