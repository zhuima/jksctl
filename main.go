package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	resty "github.com/go-resty/resty/v2"
)

var (
	url = "https://demo.com"
	mu  sync.RWMutex
)

// 获取job列表清单
type JobList struct {
	Jobs []*Job
}

type Job struct {
	Name string
	Url  string
}

// 获取job详情

type JobInfo struct {
	DisplayName     string
	FullName        string
	Buildable       bool
	NextBuildNumber int
	InQueue         bool
	Color           string //blue成功 red失败
	Url             string
}

func jenkinsClient() *resty.Request {
	c := resty.New()
	a := c.
		SetRetryCount(3).
		SetRetryWaitTime(5*time.Second).
		SetRetryWaitTime(5*time.Second).
		SetHeader("Accept", "application/json").
		R().
		SetBasicAuth(username, password)
	return a
}

// var client = jenkinsClient()

// 获取job清单
func GetJobList(job chan Job, client *resty.Request, wg *sync.WaitGroup) {
	defer wg.Done()

	mu.Lock()

	jobList := &JobList{}
	_, err := client.SetResult(jobList).Get(url + "/api/json")
	mu.Unlock()

	if err != nil {
		fmt.Errorf("get job list error", err)
	}

	// fmt.Println("resp info", resp)
	// fmt.Println("JobList", JobList)

	// 循环发送数据
	for _, v := range jobList.Jobs {
		if strings.Contains(v.Name, "-for-") {
			job <- Job{Name: v.Name, Url: v.Url}
		}
	}

}

func main() {

	count := 0
	var wg sync.WaitGroup
	var client = jenkinsClient()
	job := make(chan Job, 10)

	wg.Add(1)
	go GetJobList(job, client, &wg)

	// wg.Add(10)
	// for i := 0; i < 10; i++ {
	// 	wg.Add(1)
	// 	go GetJobList(job, &wg)

	// }

	// wg.Wait()
	// close(job)

	// 使用for循环channel的情况下，如果设置如上，会block
	// https://www.reddit.com/r/golang/comments/bpq6up/beginner_help_why_does_this_hang/
	// https://go.dev/play/p/f3VdaBnHc_C
	go func() {
		wg.Wait()
		close(job)
	}()

	for v := range job {
		fmt.Println("fp ---->", v)
		mu.Lock()
		count++
		mu.Unlock()
	}

	fmt.Println("count number", count)

}
