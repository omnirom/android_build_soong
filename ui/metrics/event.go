// Copyright 2018 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metrics

// This file contains the functionality to represent a build event in respect
// to the metric system. A build event corresponds to a block of scoped code
// that contains a "Begin()" and immediately followed by "defer End()" trace.
// When defined, the duration of the scoped code is measure along with other
// performance measurements such as memory.
//
// As explained in the metrics package, the metrics system is a stacked based
// system since the collected metrics is considered to be topline metrics.
// The steps of the build system in the UI layer is sequential. Hence, the
// functionality defined below follows the stack data structure operations.

import (
	"os"
	"syscall"
	"time"

	soong_metrics_proto "android/soong/ui/metrics/metrics_proto"

	"google.golang.org/protobuf/proto"
)

// _now wraps the time.Now() function. _now is declared for unit testing purpose.
var _now = func() time.Time {
	return time.Now()
}

// Event holds the performance metrics data of a single build event.
type Event struct {
	// The event name (mostly used for grouping a set of events)
	name string

	// The description of the event (used to uniquely identify an event
	// for metrics analysis).
	desc string

	nonZeroExitCode bool

	errorMsg *string

	// The time that the event started to occur.
	start time.Time

	// The list of process resource information that was executed.
	procResInfo []*soong_metrics_proto.ProcessResourceInfo

	m *Metrics
}

// newEvent returns an event with start populated with the now time.
func newEvent(m *Metrics, name, desc string) *Event {
	return &Event{
		name:  name,
		desc:  desc,
		start: _now(),
		m:     m,
	}
}

func (e *Event) perfInfo() *soong_metrics_proto.PerfInfo {
	realTime := uint64(_now().Sub(e.start).Nanoseconds())
	perfInfo := &soong_metrics_proto.PerfInfo{
		Description:           proto.String(e.desc),
		Name:                  proto.String(e.name),
		StartTime:             proto.Uint64(uint64(e.start.UnixNano())),
		RealTime:              proto.Uint64(realTime),
		ProcessesResourceInfo: e.procResInfo,
		NonZeroExit:           proto.Bool(e.nonZeroExitCode),
	}
	if m := e.errorMsg; m != nil {
		perfInfo.ErrorMessage = proto.String(*m)
	}
	return perfInfo
}

// End performs post calculations such as duration of the event, aggregates
// the collected performance information into PerfInfo protobuf message and
// adds it to the metrics.
func (e *Event) End() {
	e.m.SetTimeMetrics(e.perfInfo())
}

func (e *Event) SetFatalOrPanicMessage(str string) {
	e.errorMsg = &str
	e.nonZeroExitCode = true
}

// AddProcResInfo adds information on an executed process such as max resident
// set memory and the number of voluntary context switches.
func (e *Event) AddProcResInfo(name string, state *os.ProcessState) {
	rusage := state.SysUsage().(*syscall.Rusage)
	e.procResInfo = append(e.procResInfo, &soong_metrics_proto.ProcessResourceInfo{
		Name:             proto.String(name),
		UserTimeMicros:   proto.Uint64(uint64(state.UserTime().Microseconds())),
		SystemTimeMicros: proto.Uint64(uint64(state.SystemTime().Microseconds())),
		MinorPageFaults:  proto.Uint64(uint64(rusage.Minflt)),
		MajorPageFaults:  proto.Uint64(uint64(rusage.Majflt)),
		// ru_inblock and ru_oublock are measured in blocks of 512 bytes.
		IoInputKb:                  proto.Uint64(uint64(rusage.Inblock / 2)),
		IoOutputKb:                 proto.Uint64(uint64(rusage.Oublock / 2)),
		VoluntaryContextSwitches:   proto.Uint64(uint64(rusage.Nvcsw)),
		InvoluntaryContextSwitches: proto.Uint64(uint64(rusage.Nivcsw)),
	})
}
