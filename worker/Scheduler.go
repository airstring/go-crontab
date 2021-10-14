package worker

import (
	"crontab/common"
	"fmt"
	"time"
)

//任务调度
type Scheduler struct {
	jobEventChan      chan *common.JobEvent              //etcd任务事件队列
	jobPlanTable      map[string]*common.JobSchedulePlan //任务调度计划表
	jobExecutingTable map[string]*common.JobExecuteInfo  //正在执行的任务会放到这个表里
	jobResultChan     chan *common.JobExecuteResult      //任务结果队列
}

//调度协程
func (scheduler *Scheduler) scheduleLoop() {
	//for循环检测所有的任务
	var (
		jobEvent      *common.JobEvent
		scheduleAfter time.Duration
		scheduleTimer = time.NewTimer(scheduleAfter)
		jobResult     *common.JobExecuteResult
	)
	//先初始化一次（这里肯定是返回的1s，也就会睡眠1s，这个代码写或者不写都可以）
	scheduleAfter = scheduler.TrySchedule()

	//调度的延时定时器
	scheduleTimer = time.NewTimer(scheduleAfter)

	for {
		select {
		case jobEvent = <-scheduler.jobEventChan: //监听任务变化事件
			//对内存中维护的任务列表做增删改查
			scheduler.handleJobEvent(jobEvent)
		case <-scheduleTimer.C: //最近的任务到期了
		case jobResult = <-scheduler.jobResultChan: //监听任务执行结果
			scheduler.handleJobResult(jobResult)
		}
		//调度一次任务
		scheduleAfter = scheduler.TrySchedule() //case中来了一个任务事件也会走到这里，也就是说监听到任务事件或最近任务到期都会导致调度一次任务
		//重置调度间隔
		scheduleTimer.Reset(scheduleAfter)
	}
}

//推送任务变化事件
func (scheduler *Scheduler) PushJobEvent(jobEvent *common.JobEvent) {
	scheduler.jobEventChan <- jobEvent
}

//处理任务结果
func (scheduler *Scheduler) handleJobResult(result *common.JobExecuteResult) {
	var (
		jobLog *common.JobLog
	)
	//删除执行状态
	delete(scheduler.jobExecutingTable, result.ExecuteInfo.Job.Name)
	//生成执行日志
	jobLog = &common.JobLog{
		JobName:      result.ExecuteInfo.Job.Name,
		Command:      result.ExecuteInfo.Job.Command,
		Output:       string(result.Output),
		PlanTime:     result.ExecuteInfo.PlanTime.UnixNano() / 1000 / 1000,
		ScheduleTime: result.ExecuteInfo.RealTime.UnixNano() / 1000 / 1000,
		StartTime:    result.StartTime.UnixNano() / 1000 / 1000,
		EndTime:      result.EndTime.UnixNano() / 1000 / 1000,
	}
	if result.Err != nil {
		jobLog.Err = result.Err.Error()
	} else {
		jobLog.Err = ""
	}
	fmt.Println("任务执行完成：", result.ExecuteInfo.Job.Name, result.Output, result.Err)
	//这个调度任务不要插入对mongo的写入操作，因为调度任务对时间要求比较高，mongo的写入可能会耗时大，影响任务的调度，甚至将调度任务给阻塞了
	//将日志转发到单独处理日志的模块去，也就是另外一个协程去做存储操作
	//将日志丢到一个channel中
	G_logSink.Append(jobLog)
}

//处理任务事件
func (scheduler *Scheduler) handleJobEvent(jobEvent *common.JobEvent) {
	var (
		jobSchedulePlan *common.JobSchedulePlan
		jobExecuteInfo  *common.JobExecuteInfo
		jobExecuting    bool
		err             error
		jobExisted      bool
	)
	switch jobEvent.EventType {
	case common.JOB_EVENT_SAVE: //保存任务事件
		if jobSchedulePlan, err = common.BuildJobSchedulePlan(jobEvent.Job); err != nil {
			return
		}
		scheduler.jobPlanTable[jobEvent.Job.Name] = jobSchedulePlan
	case common.JOB_EVENT_DELETE: //删除任务事件
		if jobSchedulePlan, jobExisted = scheduler.jobPlanTable[jobEvent.Job.Name]; jobExisted {
			delete(scheduler.jobPlanTable, jobEvent.Job.Name)
		}
	case common.JOB_EVENT_KILL: //强杀任务事件
		//取消command的执行
		//先判断任务是否在执行中
		if jobExecuteInfo, jobExecuting = scheduler.jobExecutingTable[jobEvent.Job.Name]; jobExecuting {
			jobExecuteInfo.CancelFunc() //触发command杀死shell子进程，任务得到退出
		}

	}

}

//尝试执行任务
func (scheduler *Scheduler) TryStartJob(jobPlan *common.JobSchedulePlan) {
	//调度和执行是2件事情，调度是定期的检查下那些任务到期了
	//执行的任务可能运行很久，1分钟会调度60次，但是只能执行1次，去重防止并发就是通过jobExecutingTable这个表字段来实现的,在调度执行任务的时候会把任务放到这个表里，后续调度执行的时候会首先判断当前需要执行的任务在不在这个表里，如果在的话就不执行
	var (
		jobExecuteInfo *common.JobExecuteInfo
		jobExecuting   bool
	)

	//如果任务正在执行，跳过本次调度
	if jobExecuteInfo, jobExecuting = scheduler.jobExecutingTable[jobPlan.Job.Name]; jobExecuting {
		fmt.Println("前一次任务还未退出，将跳过执行:", jobExecuteInfo.Job.Name)
		return
	}
	//构建执行状态信息
	jobExecuteInfo = common.BuildJobExecuteInfo(jobPlan)
	//保存执行状态
	scheduler.jobExecutingTable[jobPlan.Job.Name] = jobExecuteInfo

	//执行任务
	//TODO:
	fmt.Println("执行任务:", jobExecuteInfo.Job.Name, jobExecuteInfo.PlanTime, jobExecuteInfo.RealTime)
	G_executor.ExecuteJob(jobExecuteInfo)
	//执行完成后需要将任务从jobExecutingTable中删掉

}

//回传任务执行结果
func (scheduler *Scheduler) PushJobResult(jobResult *common.JobExecuteResult) {
	scheduler.jobResultChan <- jobResult
}

func (scheduler *Scheduler) TrySchedule() (scheduleAfter time.Duration) {
	var (
		jobPlan  *common.JobSchedulePlan
		now      time.Time
		nearTime *time.Time
	)
	//如果任务表为空的话，随便睡眠多久
	if len(scheduler.jobPlanTable) == 0 {
		scheduleAfter = 1 * time.Second
		return
	}
	//当前时间
	now = time.Now()
	//遍历所有任务
	for _, jobPlan = range scheduler.jobPlanTable {
		if jobPlan.NextTime.Before(now) || jobPlan.NextTime.Equal(now) { //说明任务到期
			//TODO:尝试执行任务  尝试执行任务。为什么说是尝试执行任务呢？因为有可能任务到期了但是上一次任务执行还没有结束，所以不一定能启动
			fmt.Println("尝试执行任务:", jobPlan.Job.Name)
			scheduler.TryStartJob(jobPlan)
			jobPlan.NextTime = jobPlan.Expr.Next(now) //更新下次执行时间
		}
		//统计最近一个要过期的任务时间
		if nearTime == nil || jobPlan.NextTime.Before(*nearTime) {
			nearTime = &jobPlan.NextTime
		}

	}
	//下次调度间隔（最近要执行的任务调度时间 - 当前时间），这个就是将要睡眠的时间
	scheduleAfter = (*nearTime).Sub(now)

	return

}

var (
	G_scheduler *Scheduler
)

//初始化调度器
func InitScheduler() (err error) {
	G_scheduler = &Scheduler{
		jobEventChan:      make(chan *common.JobEvent, 1000),
		jobPlanTable:      make(map[string]*common.JobSchedulePlan),
		jobExecutingTable: make(map[string]*common.JobExecuteInfo),
		jobResultChan:     make(chan *common.JobExecuteResult, 1000),
	}
	//启动调度协程
	go G_scheduler.scheduleLoop()
	return
}
