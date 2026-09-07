package jmap

import (
	"encoding/json"
	"fmt"
)

// JMAP invocations are three-element JSON arrays, not objects with numeric keys.
func (call *MethodCall) UnmarshalJSON(data []byte) error {
	*call = MethodCall{}
	var fields []json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if len(fields) != 3 {
		return fmt.Errorf("method invocation requires three elements")
	}
	if err := json.Unmarshal(fields[0], &call.Name); err != nil {
		return err
	}
	if err := json.Unmarshal(fields[1], &call.Arguments); err != nil {
		return err
	}
	var identifier *string
	if err := json.Unmarshal(fields[2], &identifier); err != nil {
		return err
	}
	if call.Name == "" || call.Arguments == nil || identifier == nil {
		return fmt.Errorf("invalid method invocation")
	}
	call.ID = *identifier
	return nil
}
func (call MethodCall) MarshalJSON() ([]byte, error) {
	return json.Marshal([3]interface{}{call.Name, call.Arguments, call.ID})
}
func (response MethodResponse) MarshalJSON() ([]byte, error) {
	return json.Marshal([3]interface{}{response.Name, response.Arguments, response.CallID})
}
