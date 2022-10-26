package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	resty "github.com/go-resty/resty/v2"
	"github.com/gosuri/uiprogress"
	"github.com/joho/godotenv"
	"github.com/tidwall/gjson"
)

var (
	url      string
	username string
	password string
	debug    bool
	output   bool
	help     bool
	filename string
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
	//blue成功 red失败
	Color string
	Url   string
}

type Result struct {
	User              string `json:"user"`
	TimeStamp         int64  `json:"timestamp"`
	LastBuiltRevision string `json:"lastBuiltRevision,omitempty"`
	RemoteUrls        string `json:"remoteUrls,omitempty"`
	DisplayName       string `json:"displayname"`
	Buildable         bool   `json:"buildable"`
	NextBuildNumber   int    `json:"nextBuildNumber"`
	Url               string `json:"url"`
	Color             string `json:"color"`
}

func init() {
	flag.BoolVar(&debug, "d", false, "log out all the debug information")
	flag.StringVar(&url, "l", "", "Please Input jenkins address. (Required)")
	flag.StringVar(&username, "u", "", "Please Input jenkins login username. (Required)")
	flag.StringVar(&password, "p", "", "Please Input jenkins login user password. (Required)")
	flag.StringVar(&filename, "f", "jenkins-demo.json", "output filename")
	flag.BoolVar(&help, "h", false, "this help")
}

// 自定义 usage
func usage() {
	fmt.Fprintf(os.Stderr, `jksctl version: jksctl/1.0.0

Usage: 
	jksctl [-do] [-l url] [-f filename] [-u username] [-p password]


Example:

	jksctl -h // 查看帮助
	jksctl -l https://demo.com -u username -p password -f demo.json


Options:
`)
	flag.PrintDefaults()
}

// 封装 resty
func jenkinsClient() *resty.Request {
	c := resty.New()
	a := c.
		SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true}).
		SetRetryCount(3).
		SetRetryWaitTime(5*time.Second).
		SetRetryWaitTime(5*time.Second).
		R().
		SetHeader("Content-Type", "application/json").
		SetBasicAuth(username, password)
	return a
}

// 获取job name list
func GetJobList() <-chan string {
	client := jenkinsClient()
	out := make(chan string)
	jobList := &JobList{}
	_, err := client.SetResult(jobList).Get(url + "/api/json")

	printDebug(fmt.Sprintf("get job list %s", url))
	if err != nil {
		fmt.Printf("get job list error %s", err)
	}

	go func() {
		// 循环发送数据
		for _, v := range jobList.Jobs {
			// 针对不同的jenkins判断指标不一样
			if strings.Contains(url, "pipeline") && strings.Contains(v.Name, "-for-") {
				out <- v.Name
			}
			if strings.Contains(url, "tech") && strings.Contains(v.Name, "git") {
				out <- v.Name
			}
		}

		defer close(out)
	}()

	return out

}

// 获取单个job的详情
func jenkinsJobInfo(in <-chan string) <-chan JobInfo {

	client := jenkinsClient()
	out := make(chan JobInfo)

	jobinfo := &JobInfo{}

	go func() {
		for jobname := range in {
			_, err := client.SetResult(jobinfo).Get(url + "/job/" + jobname + "/api/json")
			if err != nil {
				fmt.Printf("get job info error %s", err)
			}
			out <- JobInfo{DisplayName: jobinfo.DisplayName, FullName: jobinfo.FullName, Buildable: jobinfo.Buildable, NextBuildNumber: jobinfo.NextBuildNumber,
				InQueue: jobinfo.InQueue, Color: jobinfo.Color, Url: jobinfo.Url}
		}

		defer close(out)
	}()

	return out

}

// pipeline 模式扇入
func merge(cs ...<-chan JobInfo) <-chan JobInfo {
	var wg sync.WaitGroup
	out := make(chan JobInfo)

	// Start an output goroutine for each input channel in cs.  output
	// copies values from c to out until c is closed, then calls wg.Done.
	output := func(c <-chan JobInfo) {
		for n := range c {
			out <- n
		}
		defer wg.Done()
	}
	wg.Add(len(cs))
	for _, c := range cs {
		go output(c)
	}

	// Start a goroutine to close out once all the output goroutines are
	// done.  This must start after the wg.Add call.
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

// 解析最后一次构建信息， 这里没有使用json反序列化，原因参考下面的链接，引入了gjson
// https://gist.github.com/zhuima/3b2792835e2723d1c5c272cfd68de92e
func parseUser(in <-chan JobInfo) <-chan Result {
	client := jenkinsClient()
	out := make(chan Result)

	go func() {
		for v := range in {
			// 过滤未构建过的工程，否则容易报错
			if v.Color != "notbuilt" && v.Color != "disabled" {

				realurl := fmt.Sprintf("%s/job/%s/lastBuild/api/json", url, v.FullName)
				resp, err := client.Get(realurl)

				if err != nil {
					fmt.Println(err)
				}

				// https://stackoverflow.com/questions/38874664/limiting-amount-of-data-read-in-the-response-to-a-http-get-request
				// 只读区前100个字节
				// limitedReader := &io.LimitedReader{R: bytes.NewReader(response.Body()), N: 25}
				// body, err := ioutil.ReadAll(limitedReader)
				// fmt.Println("user info", string(body))

				out <- Result{
					User:              gjson.Get(string(resp.Body()), "actions.#.causes.#.userId|0|0").String(),
					TimeStamp:         gjson.Get(string(resp.Body()), "timestamp").Int(),
					LastBuiltRevision: gjson.Get(string(resp.Body()), "actions.#.lastBuiltRevision.SHA1|0").String(),
					RemoteUrls:        gjson.Get(string(resp.Body()), "actions.#.remoteUrls|0|0").String(),
					DisplayName:       v.DisplayName,
					Buildable:         v.Buildable,
					NextBuildNumber:   v.NextBuildNumber,
					Color:             v.Color,
					Url:               v.Url}
			}
		}

		defer close(out)
	}()

	return out
}

// Write json to file
// https://blog.logrocket.com/using-json-go-guide/
// 以追加写的方式写入文件
func WriteJson(filename string, in <-chan Result) {
	printDebug(fmt.Sprintf("write result to json file %s", filename))

	f, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		panic(err)
	}

	defer f.Close()

	uiprogress.Start()            // 开始
	bar := uiprogress.AddBar(100) // 添加一个新的进度条

	// 可选，添加完成进度
	bar.AppendCompleted()
	// 可选，添加耗费时间
	bar.PrependElapsed()

	// https://stackoverflow.com/questions/7151261/append-to-a-file-in-go

	// 增加进度条的值
	for bar.Incr() {
		for user := range in {
			// bytes, _ := json.MarshalIndent(user, "", "  ")
			bytes, _ := json.Marshal(user)
			if _, err = f.WriteString(string(bytes) + "\n"); err != nil {
				panic(err)
			}
		}
	}

	fmt.Fprintf(os.Stderr, `output file %s success, please check the file`, filename)

}

// DEBUG状态下进行详细日志输出
func printDebug(msg string) {
	if debug {
		fmt.Printf("[DEBUG]: %s\n", msg)
	}
}

func main() {

	// err := gotdotenv.Load() // 👈 load .env file
	// if err != nil {
	// 	log.Fatal(err)
	// }

	flag.Parse()
	flag.Usage = usage

	// https://thedevelopercafe.com/articles/loading-environment-variables-properly-in-go-with-env-and-godotenv-7ec94d4101a7
	// https://zenn.dev/keyamin/articles/4dbcce8f214bfe
	// 从环境变量里读取配置
	godotenv.Overload()

	url = os.Getenv("URL")
	username = os.Getenv("USERNAME")
	password = os.Getenv("TOKEN")

	// if len(flag.Args()) < 1 {
	// 	flag.Usage()
	// 	fmt.Println("退出了么")
	// 	os.Exit(1)
	// }

	// 帮助信息
	if help || url == "" || username == "" || password == "" {
		fmt.Fprintf(os.Stderr, `Please Input Jenkins info, More Info You Can See help:
		
`)
		flag.Usage()
		os.Exit(1)
	}

	currentTime := time.Now()

	// 开始真正的工作
	// pipeline  step1
	in := GetJobList()

	// pipeline step2
	out1 := jenkinsJobInfo(in)
	out2 := jenkinsJobInfo(in)
	out3 := jenkinsJobInfo(in)
	out4 := jenkinsJobInfo(in)
	out5 := jenkinsJobInfo(in)
	out6 := jenkinsJobInfo(in)
	out7 := jenkinsJobInfo(in)
	out8 := jenkinsJobInfo(in)
	out9 := jenkinsJobInfo(in)
	out10 := jenkinsJobInfo(in)

	jobinfos := merge(out1, out2, out3, out4, out5, out6, out7, out8, out9, out10)

	// pipeline step3
	users := parseUser(jobinfos)

	// output to console
	// if output {
	// 	for user := range users {
	// 		fmt.Println("user info", user)
	// 	}
	// }

	// output to file
	// if !output && filename != "" && len(filename) > 1 {
	// fmt.Println("filename", filename, url, username, password)
	printDebug(fmt.Sprintf("jenkins url is %s, write file is %s", url, filename))
	WriteJson(filename, users)
	// }

	// debug
	// for name := range in {
	// 	fmt.Println("name info", name)
	// 	mu.Lock()
	// 	count++
	// 	mu.Unlock()
	// }

	fmt.Println("time cost", time.Since(currentTime))

}

// 与えられたキーの環境変数を与えられた文字列で上書きします。
// 元の環境変数と上書きする環境変数どちらも存在しない場合、errorを返します。
func overrideEnv(key, value string) error {
	if value != "" {
		os.Setenv(key, value)
		return nil
	} else if _, ok := os.LookupEnv(key); !ok {
		return fmt.Errorf("%sを指定してください。(-%s=<VALUE>)", key, key)
	}
	return nil
}
