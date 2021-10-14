//用go开发的时候用的协程，协程会被调度到线程上，线程是操作系统的概念

package main

import (
	"crontab/worker"
	"flag"
	"fmt"
	"runtime"
	"time"
)

var (
	confFile string //配置文件路径
)

//解析命令行参数
func initArgs() {
	//worker -config ./worker.json 程序最终可能这么启动
	flag.StringVar(&confFile, "config", "./worker.json", "传入worker.json")
	flag.Parse() //解析命令行参数，然后上边预定义赋值

}

func initEnv() {
	//设置线程数和cpu的核心数相等
	runtime.GOMAXPROCS(runtime.NumCPU())
}

func main() {
	var (
		err error
	)
	//初始化命令行参数
	initArgs()
	//初始化线程
	initEnv()

	//加载配置 文件名通过命令行传进来
	if err = worker.InitConfig(confFile); err != nil {
		goto ERR
	}

	if err = worker.InitRegister(); err != nil {
		goto ERR
	}
	//启动日志协程
	if err = worker.InitLogSink(); err != nil {
		goto ERR
	}
	//启动执行器
	if err = worker.InitExecutor(); err != nil {
		goto ERR
	}

	//启动调度器
	if err = worker.InitScheduler(); err != nil { //会监听事件
		goto ERR
	}
	//初始化任务管理器
	if err = worker.InitJobMgr(); err != nil { //会将事件同步给上边这个监听事件的方法
		goto ERR
	}
	for {
		time.Sleep(1 * time.Second)
	}
ERR:
	fmt.Println(err)
}
