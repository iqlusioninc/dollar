// SPDX-License-Identifier: BUSL-1.1
//
// Minimal shim that satisfies the gogo protobuf generated interfaces until the
// upstream dependency is available in this workspace.
package grpc

import "google.golang.org/grpc"

// ClientConn matches the expectations of protoc-gen-gogo generated stubs.
type ClientConn = grpc.ClientConnInterface

// Server matches the expectations of protoc-gen-gogo generated stubs.
type Server = grpc.ServiceRegistrar

// CallOption preserves the call option type used by generated code.
type CallOption = grpc.CallOption

// StreamDesc preserves the streaming descriptor type used by generated code.
type StreamDesc = grpc.StreamDesc

// ClientStream preserves the client stream type used by generated code.
type ClientStream = grpc.ClientStream

