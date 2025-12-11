// Copyright 2017 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package build

import (
	"context"
	"io"

	"android/soong/ui/execution_metrics"
	"android/soong/ui/logger"
	"android/soong/ui/metrics"
	soong_metrics_proto "android/soong/ui/metrics/metrics_proto"
	"android/soong/ui/status"
	"android/soong/ui/tracer"
)

// Context combines a context.Context, logger.Logger, and terminal.Writer.
// These all are agnostic of the current build, and may be used for multiple
// builds, while the Config objects contain per-build information.
type Context struct{ *ContextImpl }
type ContextImpl struct {
	context.Context
	logger.Logger

	Metrics          *metrics.Metrics
	ExecutionMetrics *execution_metrics.ExecutionMetrics

	Writer io.Writer
	Status *status.Status

	Thread tracer.Thread
	Tracer tracer.Tracer

	CriticalPath *status.CriticalPath
}

// BeginTrace starts a new Duration Event.  Call End on the returned TraceEvent
// to end the Event.
func (c ContextImpl) BeginTrace(name, desc string) *TraceEvent {
	e := &TraceEvent{
		c: &c,
	}
	if c.Tracer != nil {
		c.Tracer.Begin(desc, c.Thread)
	}
	if c.Metrics != nil {
		e.Event = c.Metrics.Begin(name, desc)
	}
	return e
}

type TraceEvent struct {
	c *ContextImpl
	*metrics.Event
}

func (e *TraceEvent) End() {
	if e.c.Tracer != nil {
		e.c.Tracer.End(e.c.Thread)
	}
	if e.c.Metrics != nil {
		e.Event.End()
	}
}

// CompleteTrace writes a trace with a beginning and end times.
func (c ContextImpl) CompleteTrace(name, desc string, begin, end uint64) {
	if c.Tracer != nil {
		c.Tracer.Complete(desc, c.Thread, begin, end)
	}
	if c.Metrics != nil {
		realTime := end - begin
		c.Metrics.SetTimeMetrics(
			&soong_metrics_proto.PerfInfo{
				Description: &desc,
				Name:        &name,
				StartTime:   &begin,
				RealTime:    &realTime})
	}
}

// ExecutionMetricsFinishAdaptor wraps Context to adapt the BeginTrace method to match the
// execution_metrics.hasTrace interface.  This is a workaround to avoid a circular dependency
// between the build and execution_metrics packages.
type ExecutionMetricsFinishAdaptor struct {
	Context
}

// BeginTrace adapts Context.BeginTrace to match the execution_metrics.hasTrace interface.
func (e ExecutionMetricsFinishAdaptor) BeginTrace(name, desc string) execution_metrics.Event {
	event := e.Context.BeginTrace(name, desc)
	return ExecutionMetricsEventAdaptor{event}
}

// ExecutionMetricsEventAdaptor wraps TraceEvent to match the execution_metrics.Event interface.
type ExecutionMetricsEventAdaptor struct {
	*TraceEvent
}

// End adapts the TraceEvent.End method to match the execution_metrics.Event interface.
func (e ExecutionMetricsEventAdaptor) End() {
	e.TraceEvent.End()
}
