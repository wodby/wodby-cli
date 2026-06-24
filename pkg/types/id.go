package types

import (
	"encoding/json"
	"fmt"
	"strconv"
)

type ID string

func ToID(v interface{}) ID {
	switch value := v.(type) {
	case ID:
		return value
	case string:
		return ID(value)
	case int:
		return ID(strconv.Itoa(value))
	case int8:
		return ID(strconv.FormatInt(int64(value), 10))
	case int16:
		return ID(strconv.FormatInt(int64(value), 10))
	case int32:
		return ID(strconv.FormatInt(int64(value), 10))
	case int64:
		return ID(strconv.FormatInt(value, 10))
	case uint:
		return ID(strconv.FormatUint(uint64(value), 10))
	case uint8:
		return ID(strconv.FormatUint(uint64(value), 10))
	case uint16:
		return ID(strconv.FormatUint(uint64(value), 10))
	case uint32:
		return ID(strconv.FormatUint(uint64(value), 10))
	case uint64:
		return ID(strconv.FormatUint(value, 10))
	default:
		return ""
	}
}

func (id ID) GetGraphQLType() string {
	return "ID"
}

func (id ID) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(id))
}

func (id *ID) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*id = ""
		return nil
	}

	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*id = ID(str)
		return nil
	}

	var number json.Number
	if err := json.Unmarshal(data, &number); err == nil {
		*id = ID(number.String())
		return nil
	}

	return fmt.Errorf("invalid id value: %s", data)
}

func (id ID) String() string {
	return string(id)
}
