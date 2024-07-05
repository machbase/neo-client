package internal

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/machbase/neo-client/pkg/pstag/report"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/sensors"
)

func CpuInput() ([]*report.Record, error) {
	v, err := cpu.Percent(0, false)
	if err != nil {
		return nil, err
	}
	ret := []*report.Record{}
	for _, p := range v {
		ret = append(ret, &report.Record{Name: "cpu.percent", Value: p, Precision: 1})
	}
	return ret, nil
}

func LoadInput() ([]*report.Record, error) {
	stat, err := load.Avg()
	if err != nil {
		return nil, err
	}
	ret := []*report.Record{
		{Name: "load1", Value: stat.Load1, Precision: 2},
		{Name: "load5", Value: stat.Load5, Precision: 2},
		{Name: "load15", Value: stat.Load15, Precision: 2},
	}
	return ret, nil
}

func MemInput() ([]*report.Record, error) {
	stat, err := mem.VirtualMemory()
	if err != nil {
		return nil, err
	}
	ret := []*report.Record{
		{Name: "mem.total", Value: float64(stat.Total), Precision: 0},
		{Name: "mem.free", Value: float64(stat.Free), Precision: 0},
		{Name: "mem.used", Value: float64(stat.Used), Precision: 0},
		{Name: "mem.used_percent", Value: stat.UsedPercent, Precision: 1},
	}
	return ret, nil
}

func DiskInput(args []string) func() ([]*report.Record, error) {
	mountpoints := strings.Split(args[0], ",")
	return func() ([]*report.Record, error) {
		stat, err := disk.Partitions(false)
		if err != nil {
			return nil, err
		}
		ret := []*report.Record{}
		for _, v := range stat {
			matched := false
			for _, point := range mountpoints {
				if point == "all" || point == v.Mountpoint {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			usage, err := disk.Usage(v.Mountpoint)
			if err != nil {
				return nil, err
			}
			ret = append(ret,
				&report.Record{
					Name:      fmt.Sprintf("disk.%s.total", v.Mountpoint),
					Value:     float64(usage.Total),
					Precision: 0,
				},
				&report.Record{
					Name:      fmt.Sprintf("disk.%s.free", v.Mountpoint),
					Value:     float64(usage.Free),
					Precision: 0,
				},
				&report.Record{
					Name:      fmt.Sprintf("disk.%s.used", v.Mountpoint),
					Value:     float64(usage.Used),
					Precision: 0,
				},
				&report.Record{
					Name:      fmt.Sprintf("disk.%s.used_percent", v.Mountpoint),
					Value:     usage.UsedPercent,
					Precision: 1,
				},
			)
			if runtime.GOOS != "windows" {
				ret = append(ret,
					&report.Record{
						Name:      fmt.Sprintf("disk.%s.inodes_total", v.Mountpoint),
						Value:     float64(usage.InodesTotal),
						Precision: 0,
					},
					&report.Record{
						Name:      fmt.Sprintf("disk.%s.inodes_free", v.Mountpoint),
						Value:     float64(usage.InodesFree),
						Precision: 0,
					},
					&report.Record{
						Name:      fmt.Sprintf("disk.%s.inodes_used", v.Mountpoint),
						Value:     float64(usage.InodesUsed),
						Precision: 0,
					},
					&report.Record{
						Name:      fmt.Sprintf("disk.%s.inodes_used_percent", v.Mountpoint),
						Value:     usage.InodesUsedPercent,
						Precision: 1,
					},
				)
			}
		}
		return ret, nil
	}
}

func DiskioInput(args []string) func() ([]*report.Record, error) {
	devPatterns := strings.Split(args[0], ",")
	return func() ([]*report.Record, error) {
		stat, err := disk.IOCounters()
		if err != nil {
			return nil, err
		}
		ret := []*report.Record{}
		for _, v := range stat {
			matched := false
			for _, pattern := range devPatterns {
				if ok, err := filepath.Match(pattern, v.Name); !ok || err != nil {
					continue
				} else {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			ret = append(ret,
				&report.Record{
					Name:      fmt.Sprintf("diskio.%s.read_bytes", v.Name),
					Value:     float64(v.ReadBytes),
					Precision: 0,
				},
				&report.Record{
					Name:      fmt.Sprintf("diskio.%s.write_bytes", v.Name),
					Value:     float64(v.WriteBytes),
					Precision: 0,
				},
			)
		}
		return ret, nil
	}
}

func NetInput(args []string) func() ([]*report.Record, error) {
	nicPatterns := strings.Split(args[0], ",")
	return func() ([]*report.Record, error) {
		stat, err := net.IOCounters(true)
		if err != nil {
			return nil, err
		}
		ret := []*report.Record{}
		for _, v := range stat {
			matched := false
			for _, pattern := range nicPatterns {
				if ok, err := filepath.Match(pattern, v.Name); !ok || err != nil {
					continue
				} else {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			ret = append(ret,
				&report.Record{
					Name:      fmt.Sprintf("net.%s.bytes_sent", v.Name),
					Value:     float64(v.BytesSent),
					Precision: 0,
				},
				&report.Record{
					Name:      fmt.Sprintf("net.%s.bytes_recv", v.Name),
					Value:     float64(v.BytesRecv),
					Precision: 0,
				},
				&report.Record{
					Name:      fmt.Sprintf("net.%s.packets_sent", v.Name),
					Value:     float64(v.PacketsSent),
					Precision: 0,
				},
				&report.Record{
					Name:      fmt.Sprintf("net.%s.packet_recv", v.Name),
					Value:     float64(v.PacketsRecv),
					Precision: 0,
				},
				&report.Record{
					Name:      fmt.Sprintf("net.%s.drop_in", v.Name),
					Value:     float64(v.Dropin),
					Precision: 0,
				},
				&report.Record{
					Name:      fmt.Sprintf("net.%s.drop_out", v.Name),
					Value:     float64(v.Dropout),
					Precision: 0,
				},
			)
		}
		return ret, nil
	}
}

func ProtoInput(args []string) func() ([]*report.Record, error) {
	protos := strings.Split(args[0], ",")
	return func() ([]*report.Record, error) {
		stat, err := net.ProtoCounters(protos)
		if err != nil {
			return nil, err
		}
		ret := []*report.Record{}
		for _, st := range stat {
			for k, v := range st.Stats {
				ret = append(ret,
					&report.Record{
						Name:      fmt.Sprintf("proto.%s.%s", st.Protocol, k),
						Value:     float64(v),
						Precision: 0,
					},
				)
			}
		}
		return ret, nil
	}
}

func SensorInput() ([]*report.Record, error) {
	stat, err := sensors.SensorsTemperatures()
	if err != nil {
		return nil, err
	}

	ret := []*report.Record{}
	for _, v := range stat {
		ret = append(ret,
			&report.Record{
				Name:      fmt.Sprintf("sensor.%s.temperature", v.SensorKey),
				Value:     v.Temperature,
				Precision: 1,
			},
			/*
				&report.Record{
					Name:      fmt.Sprintf("sensor.%s.high", v.SensorKey),
					Value:     v.High,
					Precision: 1,
				},
				&report.Record{
					Name:      fmt.Sprintf("sensor.%s.critical", v.SensorKey),
					Value:     v.Critical,
					Precision: 1,
				},
			*/
		)
	}
	return ret, nil
}

func HostInput() ([]*report.Record, error) {
	stat, err := host.Info()
	if err != nil {
		return nil, err
	}
	ret := []*report.Record{
		{Name: "host.uptime", Value: float64(stat.Uptime), Precision: 0},
		{Name: "host.procs", Value: float64(stat.Procs), Precision: 0},
	}
	return ret, nil
}
