package control

import (
	"encoding/json"
	"fmt"
	"net"
)

func jsonEncoder(conn net.Conn, command Command) error {
	if err := json.NewEncoder(conn).Encode(command); err != nil {
		return fmt.Errorf("write control command: %w", err)
	}
	return nil
}
