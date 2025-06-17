package execution_metrics_proto

import (
	"testing"

	find_input_delta_proto "android/soong/cmd/find_input_delta/find_input_delta_proto"
)

func TestExecutionMetricsMessageNums(t *testing.T) {
	testCases := []struct {
		Name         string
		FieldNumbers map[string]int32
	}{
		{
			Name:         "find_input_delta_proto",
			FieldNumbers: find_input_delta_proto.FieldNumbers_value,
		},
	}
	verifiedMap := make(map[string]bool)
	for _, tc := range testCases {
		for k, v := range tc.FieldNumbers {
			if FieldNumbers_value[k] != v {
				t.Errorf("%s: Expected FieldNumbers.%s == %v, found %v", tc.Name, k, FieldNumbers_value[k], v)
			}
			verifiedMap[k] = true
		}
	}
	for k, v := range FieldNumbers_value {
		if !verifiedMap[k] {
			t.Errorf("No test case verifies FieldNumbers.%s=%v", k, v)
		}
	}
}
