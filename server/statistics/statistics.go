// Package statistics 统计数据
package statistics

import (
	"fmt"
	"go-stress-testing/helper"
	"go-stress-testing/tools"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/language"
	"golang.org/x/text/message"

	"go-stress-testing/model"
)

var (
	// 输出统计数据的时间
	exportStatisticsTime = 1 * time.Second
	p                    = message.NewPrinter(language.English)
	RequestTimeList      []uint64 //所有请求响应时间
)

// ReceivingResults 接收结果并处理
// 统计的时间都是纳秒，显示的时间 都是毫秒
// concurrent 并发数
func ReceivingResults(concurrent uint64, ch <-chan *model.RequestResults, wg *sync.WaitGroup) {
	defer func() {
		wg.Done()
	}()
	var stopChan = make(chan bool)
	// 时间
	var (
		processingTime uint64 // 处理总时间
		requestTime    uint64 // 请求总时间
		maxTime        uint64 // 最大时长
		minTime        uint64 // 最小时长
		successNum     uint64 // 成功处理数，code为0
		failureNum     uint64 // 处理失败数，code不为0
		chanIDLen      int    // 并发数
		chanIDs        = make(map[uint64]bool)
		receivedBytes  int64
	)
	statTime := uint64(time.Now().UnixNano())
	// 错误码/错误个数
	var errCode = make(map[int]int)
	// 定时输出一次计算结果
	ticker := time.NewTicker(exportStatisticsTime)
	go func() {
		for {
			select {
			case <-ticker.C:
				endTime := uint64(time.Now().UnixNano())
				requestTime = endTime - statTime
				go calculateData(concurrent, processingTime, requestTime, maxTime, minTime, successNum, failureNum,
					chanIDLen, errCode, receivedBytes)
			case <-stopChan:
				// 处理完成
				return
			}
		}
	}()
	header()
	for data := range ch {
		// fmt.Println("处理一条数据", data.ID, data.Time, data.IsSucceed, data.ErrCode)
		processingTime = processingTime + data.Time
		if maxTime <= data.Time {
			maxTime = data.Time
		}
		if minTime == 0 {
			minTime = data.Time
		} else if minTime > data.Time {
			minTime = data.Time
		}
		// 是否请求成功
		if data.IsSucceed == true {
			successNum = successNum + 1
		} else {
			failureNum = failureNum + 1
		}
		// 统计错误码
		if value, ok := errCode[data.ErrCode]; ok {
			errCode[data.ErrCode] = value + 1
		} else {
			errCode[data.ErrCode] = 1
		}
		receivedBytes += data.ReceivedBytes
		if _, ok := chanIDs[data.ChanID]; !ok {
			chanIDs[data.ChanID] = true
			chanIDLen = len(chanIDs)
		}
	}
	// 数据全部接受完成，停止定时输出统计数据
	stopChan <- true
	endTime := uint64(time.Now().UnixNano())
	requestTime = endTime - statTime
	calculateData(concurrent, processingTime, requestTime, maxTime, minTime, successNum, failureNum, chanIDLen, errCode,
		receivedBytes)
	//排序后计算 tp50 75 90 95 99
	all := tools.MyUint64List{}
	all = RequestTimeList
	sort.Sort(all)

	// 输出表格
	concurrentTotal := strconv.FormatInt(int64(concurrent),10)
	requestTotal := strconv.FormatInt(int64(successNum+failureNum),10)
	requestTimeTotal := fmt.Sprintf("%.3f", float64(requestTime)/1e9)
	successTotal :=  strconv.FormatInt(int64(successNum),10)
	failureTotal :=  strconv.FormatInt(int64(failureNum),10)
	tp90 := fmt.Sprintf("%.3f", float64(all[int(float64(len(all))*0.90)]/1e6))
	tp95 := fmt.Sprintf("%.3f", float64(all[int(float64(len(all))*0.95)]/1e6))
	tp99 := fmt.Sprintf("%.3f", float64(all[int(float64(len(all))*0.99)]/1e6))

	fmt.Printf("\n\n")
	fmt.Println("*************************  结果 start  ****************************")
	fmt.Println("处理协程数量:", concurrentTotal)
	// fmt.Println("处理协程数量:", concurrent, "程序处理总时长:", fmt.Sprintf("%.3f", float64(processingTime/concurrent)/1e9), "秒")
	fmt.Println("请求总数（并发数*请求数 -c * -n）:", requestTotal, "总请求时间:",requestTimeTotal,
		"秒", "successNum:", successTotal, "failureNum:", failureTotal)
	fmt.Println("tp90:", tp90)
	fmt.Println("tp95:", tp95)
	fmt.Println("tp99:", tp99)
	fmt.Println("*************************  结果 end   ****************************")
	fmt.Printf("\n\n")

	helper.OutputResult("<h5>输出结果</h5></table><br/>")
	helper.OutputResult(
		"<table border='1'>" +
				"<tr>" +
					"<th>统计</th>" +
					"<th>处理协程数量</th>" +
					"<th>请求总数（并发数*请求数 -c * -n）</th>" +
					"<th>总请求时间" +
					"<th>请求成功数" +
					"<th>请求失败数" +
					"<th>tp90" +
					"<th>tp95" +
					"<th>tp99" +
				"</tr>" +
				"<tr>" +
					"<td>结果</td>" +
					"<td>"+concurrentTotal +"</td>" +
					"<td>"+requestTotal +"</td>" +
					"<td>"+requestTimeTotal +"秒</td>" +
					"<td>"+successTotal +"</td>" +
					"<td>"+failureTotal +"</td>" +
					"<td>"+tp90 +"</td>" +
					"<td>"+tp95 +"</td>" +
					"<td>"+tp99 +"</td>" +
				"</tr>" +
			"</table><br/>")
}

// calculateData 计算数据
func calculateData(concurrent, processingTime, requestTime, maxTime, minTime, successNum, failureNum uint64,
	chanIDLen int, errCode map[int]int, receivedBytes int64) {
	if processingTime == 0 {
		processingTime = 1
	}
	var (
		qps              float64
		averageTime      float64
		maxTimeFloat     float64
		minTimeFloat     float64
		requestTimeFloat float64
	)
	// 平均 每个协程成功数*总协程数据/总耗时 (每秒)
	if processingTime != 0 {
		qps = float64(successNum*1e9*concurrent) / float64(processingTime)
	}
	// 平均时长 总耗时/总请求数/并发数 纳秒=>毫秒
	if successNum != 0 && concurrent != 0 {
		averageTime = float64(processingTime) / float64(successNum*1e6)
	}
	// 纳秒=>毫秒
	maxTimeFloat = float64(maxTime) / 1e6
	minTimeFloat = float64(minTime) / 1e6
	requestTimeFloat = float64(requestTime) / 1e9
	// 打印的时长都为毫秒
	table(successNum, failureNum, errCode, qps, averageTime, maxTimeFloat, minTimeFloat, requestTimeFloat, chanIDLen,
		receivedBytes)
}

// header 打印表头信息
func header() {
	fmt.Printf("\n\n")
	// 打印的时长都为毫秒 总请数
	fmt.Println("─────┬───────┬───────┬───────┬────────┬────────┬────────┬────────┬────────┬────────┬────────")
	fmt.Println(" 耗时│ 并发数│ 成功数│ 失败数│   qps  │最长耗时│最短耗时│平均耗时│下载字节│字节每秒│ 状态码")
	fmt.Println("─────┼───────┼───────┼───────┼────────┼────────┼────────┼────────┼────────┼────────┼────────")
	outputHeader() // 输出表格头
	return
}

// outputHeader 输出表格头
func outputHeader()  {
	helper.OutputResult(
		"<h5>调用过程</h5><table border='1'>" +
				"<tr>" +
					"<th>请求详情</th>" +
					"<th>耗时</th>" +
					"<th>并发数</th>" +
					"<th>成功数</th>" +
					"<th>失败数</th>" +
					"<th>qps</th>" +
					"<th>最长耗时</th>" +
					"<th>最短耗时</th>" +
					"<th>平均耗时</th>" +
					"<th>下载字节</th>" +
					"<th>字节每秒</th>" +
					"<th>状态码</th>" +
				"</tr>")
}

// table 打印表格
func table(successNum, failureNum uint64, errCode map[int]int,
	qps, averageTime, maxTimeFloat, minTimeFloat, requestTimeFloat float64, chanIDLen int, receivedBytes int64) {
	var (
		speed int64
	)
	if requestTimeFloat > 0 {
		speed = int64(float64(receivedBytes) / requestTimeFloat)
	} else {
		speed = 0
	}
	var (
		receivedBytesStr string
		speedStr         string
	)
	// 判断获取下载字节长度是否是未知
	if receivedBytes <= 0 {
		receivedBytesStr = ""
		speedStr = ""
	} else {
		receivedBytesStr = p.Sprintf("%d", receivedBytes)
		speedStr = p.Sprintf("%d", speed)
	}
	// 打印的时长都为毫秒
	result := fmt.Sprintf("%4.0fs│%7d│%7d│%7d│%8.2f│%8.2f│%8.2f│%8.2f│%8s│%8s│%v",
		requestTimeFloat, chanIDLen, successNum, failureNum, qps, maxTimeFloat, minTimeFloat, averageTime,
		receivedBytesStr, speedStr,
		printMap(errCode))
	helper.OutputResult(
		"<tr/>" +
				"<td>详情</td>" +
				"<td>"+fmt.Sprintf("%4.0fs",requestTimeFloat) +"</td>" +
				"<td>"+fmt.Sprintf("%7d",chanIDLen) +"</td>" +
				"<td>"+fmt.Sprintf("%7d",successNum) +"</td>" +
				"<td>"+fmt.Sprintf("%7d",failureNum) +"</td>" +
				"<td>"+fmt.Sprintf("%8.2f",qps) +"</td>" +
				"<td>"+fmt.Sprintf("%8.2f",maxTimeFloat) +"</td>" +
				"<td>"+fmt.Sprintf("%8.2f",minTimeFloat) +"</td>" +
				"<td>"+fmt.Sprintf("%8.2f",averageTime) +"</td>" +
				"<td>"+fmt.Sprintf("%8s",receivedBytesStr) +"</td>" +
				"<td>"+fmt.Sprintf("%8s",speedStr) +"</td>" +
				"<td>"+fmt.Sprintf("%v",printMap(errCode)) +"</td>" +
			"<tr/>")

	fmt.Println(result)
	return
}

// printMap 输出错误码、次数 节约字符(终端一行字符大小有限)
func printMap(errCode map[int]int) (mapStr string) {
	var (
		mapArr []string
	)
	for key, value := range errCode {
		mapArr = append(mapArr, fmt.Sprintf("%d:%d", key, value))
	}
	sort.Strings(mapArr)
	mapStr = strings.Join(mapArr, ";")
	return
}