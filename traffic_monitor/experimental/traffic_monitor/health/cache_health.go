package health

import (
	"github.com/Comcast/traffic_control/traffic_monitor/experimental/traffic_monitor/cache"
	traffic_ops "github.com/Comcast/traffic_control/traffic_ops/client"

	"fmt"
	"strconv"
	"strings"
)

// Get the String value of one of those pesky map[string]interface{} things that seem so easy
func getString(key string, intface map[string]interface{}) (string, error) {
	str, ok := intface[key].(string)

	if ok {
		return str, nil
	} else {
		return "", fmt.Errorf("Error in getString: No string found for key %s", key)
	}
}

// Get the float64 value of one of those pesky map[string]interface{} things that seem so easy
func getNumber(key string, intface map[string]interface{}) (float64, error) {
	val, ok := intface[key].(float64)

	if ok {
		return val, nil
	} else {
		return -1, fmt.Errorf("Error in getNumber: No number found for %s", key)
	}
}

func setError(newResult *cache.Result, err error) {
	newResult.Errors = append(newResult.Errors, err)
	newResult.Available = false
}

// Get the vitals to decide health on in the right format
func GetVitals(newResult *cache.Result, prevResult *cache.Result, mc *traffic_ops.TrafficMonitorConfigMap) {

	// proc.loadavg -- we're using the 1 minute average (!?)
	// value looks like: "0.20 0.07 0.07 1/967 29536" (without the quotes)
	loadAverages := strings.Fields(newResult.Astats.System.ProcLoadavg)
	if len(loadAverages) > 0 {
		oneMinAvg, err := strconv.ParseFloat(loadAverages[0], 64)
		if err != nil {
			setError(newResult, fmt.Errorf("Error converting load average string: %v", err))
			return
		}
		newResult.Vitals.LoadAvg = oneMinAvg
	} else {
		setError(newResult, fmt.Errorf("Can't make sense of'", newResult.Astats.System.ProcLoadavg, "'as a load average for", newResult.Id))
		return
	}

	// proc.net.dev -- need to compare to prevSample
	// value looks like
	// "bond0:8495786321839 31960528603    0    0    0     0          0   2349716 143283576747316 101104535041    0    0    0     0       0          0"
	// (without the quotes)
	parts := strings.Split(newResult.Astats.System.ProcNetDev, ":")
	if len(parts) > 1 {
		numbers := strings.Fields(parts[1])
		var err error
		newResult.Vitals.BytesOut, err = strconv.ParseInt(numbers[8], 10, 64)
		if err != nil {
			setError(newResult, err)
			setError(newResult, fmt.Errorf("Error converting BytesOut from procnetdev: %v", err))
			return
		}
		newResult.Vitals.BytesIn, err = strconv.ParseInt(numbers[0], 10, 64)
		if err != nil {
			setError(newResult, err)
			setError(newResult, fmt.Errorf("Error converting BytesIn from procnetdev: %v", err))
			return
		}
		if prevResult != nil && prevResult.Vitals.BytesOut != 0 {
			elapsedTimeInSecs := float64(newResult.Time.UnixNano()-prevResult.Time.UnixNano()) / 1000000000
			newResult.Vitals.KbpsOut = int64(float64(((newResult.Vitals.BytesOut - prevResult.Vitals.BytesOut) * 8 / 1000)) / elapsedTimeInSecs)
		} else {
			// fmt.Println("prevResult == nil for id " + newResult.Id + ". Hope we're just starting up?")
		}
	} else {
		setError(newResult, fmt.Errorf("Error parsing procnetdev: no fields found"))
		return
	}

	// inf.speed -- value looks like "10000" (without the quotes) so it is in Mbps.
	// TODO JvD: Should we really be running this code every second for every cache polled????? I don't think so.
	interfaceBandwidth := newResult.Astats.System.InfSpeed
	newResult.Vitals.MaxKbpsOut = int64(interfaceBandwidth)*1000 - mc.Profile[mc.TrafficServer[newResult.Id].Profile].Parameters.MinFreeKbps

	// fmt.Println(newResult.Id, "BytesOut", newResult.Vitals.BytesOut, "BytesIn", newResult.Vitals.BytesIn, "Kbps", newResult.Vitals.KbpsOut, "max", newResult.Vitals.MaxKbpsOut)
}

func EvalCache(result cache.Result, mc *traffic_ops.TrafficMonitorConfigMap) bool {

	status := mc.TrafficServer[result.Id].Status
	if status == "ADMIN_DOWN" {
		return false
	} else if status == "OFFLINE" {
		return false
	} else if status == "ONLINE" {
		return true
	}
	isAvailable := result.Available

	//fmt.Println("--", mc.TrafficServer[result.Id].Profile)
	//fmt.Println("min: " , mc.Profile[mc.TrafficServer[result.Id].Profile].Parameters.MinFreeKbps)

	if mc.Profile[mc.TrafficServer[result.Id].Profile].Parameters.HealthThresholdLoadAvg < result.Vitals.LoadAvg {
		fmt.Println(result.Id, "- setting local status to bad due to load avg of ", result.Vitals.LoadAvg)
		return false
	}

	if result.Vitals.MaxKbpsOut < result.Vitals.KbpsOut {
		fmt.Println(result.Id, "- setting local status to bad due to too high KbpsOut max:", result.Vitals.MaxKbpsOut, "current:", result.Vitals.KbpsOut)
		return false
	}
	return isAvailable
}