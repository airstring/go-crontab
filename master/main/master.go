//用go开发的时候用的协程，协程会被调度到线程上，线程是操作系统的概念

package main

import (
	"crontab/master"
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
	//master -config ./master.json 程序最终可能这么启动
	flag.StringVar(&confFile, "config", "./master.json", "传入master.json")
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
	if err = master.InitConfig(confFile); err != nil {
		goto ERR
	}
	//初始化服务发现模块（查看etcd中worker的注册ip）
	if err = master.InitWorkerMgr(); err != nil {
		goto ERR
	}
	//任务管理器
	if err = master.InitJobMgr(); err != nil {
		goto ERR
	}
	//初始化日志管理器
	if err = master.InitLogMgr(); err != nil {
		goto ERR
	}

	//启动Api HTTP服务
	if err = master.InitApiServer(); err != nil {
		goto ERR
	}
	//正常退出
	for {

		time.Sleep(1 * time.Second)
	}

ERR:
	fmt.Println(err)
}
