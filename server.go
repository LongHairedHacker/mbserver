// Package mbserver implments a Modbus server (slave).
package mbserver

import (
	"fmt"
	"io"
	"net"

	"github.com/goburrow/serial"
)

type functionCodeTable [256](func(*Server, Framer) ([]byte, *Exception))

// Server is a Modbus slave with allocated memory for discrete inputs, coils, etc.
type Server struct {
	// Debug enables more verbose messaging.
	Debug            bool
	listeners        []net.Listener
	ports            []serial.Port
	requestChan      chan *Request
	function         map[uint8]functionCodeTable
	DiscreteInputs   map[uint8][]byte
	Coils            map[uint8][]byte
	HoldingRegisters map[uint8][]uint16
	InputRegisters   map[uint8][]uint16
}

// Request contains the connection and Modbus frame.
type Request struct {
	conn  io.ReadWriteCloser
	frame Framer
}

// NewServer creates a new Modbus server (slave).
func NewServer(slaveIDs []uint8) *Server {
	s := &Server{}

	// Allocate Modbus memory maps.
	s.DiscreteInputs = make(map[uint8][]byte)
	s.Coils = make(map[uint8][]byte)
	s.HoldingRegisters = make(map[uint8][]uint16)
	s.InputRegisters = make(map[uint8][]uint16)
	s.function = make(map[uint8]functionCodeTable)

	for _, ID := range slaveIDs {
		// Allocate Modbus memory maps.
		s.DiscreteInputs[ID] = make([]byte, 65536)
		s.Coils[ID] = make([]byte, 65536)
		s.HoldingRegisters[ID] = make([]uint16, 65536)
		s.InputRegisters[ID] = make([]uint16, 65536)
		s.function[ID] = functionCodeTable{}

		// Add default functions.
		s.RegisterFunctionHandler(ID, 1, ReadCoils)
		s.RegisterFunctionHandler(ID, 2, ReadDiscreteInputs)
		s.RegisterFunctionHandler(ID, 3, ReadHoldingRegisters)
		s.RegisterFunctionHandler(ID, 4, ReadInputRegisters)
		s.RegisterFunctionHandler(ID, 5, WriteSingleCoil)
		s.RegisterFunctionHandler(ID, 6, WriteHoldingRegister)
		s.RegisterFunctionHandler(ID, 15, WriteMultipleCoils)
		s.RegisterFunctionHandler(ID, 16, WriteHoldingRegisters)
	}

	s.requestChan = make(chan *Request)
	go s.handler()

	return s
}

// RegisterFunctionHandler override the default behavior for a given Modbus function.
func (s *Server) RegisterFunctionHandler(slaveID uint8, funcCode uint8, function func(*Server, Framer) ([]byte, *Exception)) error {
	table, exists := s.function[slaveID]
	if !exists {
		return fmt.Errorf("Unable to register function for undefined slave ID: %d", slaveID)
	}
	table[funcCode] = function
	s.function[slaveID] = table
	return nil
}

func (s *Server) handle(request *Request) Framer {
	var exception *Exception
	var data []byte

	response := request.frame.Copy()

	function := request.frame.GetFunction()
	slaveID := request.frame.GetSlaveID()
	functions, exists := s.function[slaveID]
	if !exists {
		exception = &SlaveDeviceFailure
	} else {
		if functions[function] != nil {
			data, exception = s.function[slaveID][function](s, request.frame)
			response.SetData(data)
		} else {
			exception = &IllegalFunction
		}
	}

	if exception != &Success {
		response.SetException(exception)
	}

	return response
}

// All requests are handled synchronously to prevent modbus memory corruption.
func (s *Server) handler() {
	for {
		request := <-s.requestChan
		response := s.handle(request)
		request.conn.Write(response.Bytes())
	}
}

// Close stops listening to TCP/IP ports and closes serial ports.
func (s *Server) Close() {
	for _, listen := range s.listeners {
		listen.Close()
	}
	for _, port := range s.ports {
		port.Close()
	}
}
