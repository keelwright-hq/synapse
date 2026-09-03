package protobuf

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/keelwright-hq/synapse/internal/graph"
	"github.com/keelwright-hq/synapse/internal/parse"
)

// OperationSymbol is the URI/legacy symbol for an RPC (e.g. "UserService.ListUsers").
func OperationSymbol(service, method string) string {
	return service + "." + method
}

// OperationID builds a Phase-1 node id for an RPC.
func OperationID(specPath, service, method string) graph.NodeID {
	return graph.NodeID(fmt.Sprintf("operation:%s#%s", specPath, OperationSymbol(service, method)))
}

// ServiceID builds a Phase-1 node id for a service.
func ServiceID(specPath, name string) graph.NodeID {
	return graph.NodeID(fmt.Sprintf("service:%s#%s", specPath, name))
}

// SchemaID builds a Phase-1 node id for a message.
func SchemaID(specPath, name string) graph.NodeID {
	return graph.NodeID(fmt.Sprintf("schema:%s#%s", specPath, name))
}

// FieldID builds a Phase-1 node id for a message field.
func FieldID(specPath, message, field string) graph.NodeID {
	return graph.NodeID(fmt.Sprintf("field:%s#%s.%s", specPath, message, field))
}

// GRPCPath builds the conventional gRPC method path: /{package}.{Service}/{Method}.
func GRPCPath(pkg, service, method string) string {
	full := service
	if pkg != "" {
		full = pkg + "." + service
	}
	return "/" + full + "/" + method
}

// ToResult maps a loaded protobuf file into Synapse parse IR for the given
// repo-relative spec path (slash-separated). Only definitions from this file
// are emitted (imports resolve types but are not re-emitted).
func ToResult(specPath string, fd *FileDesc) parse.Result {
	specPath = filepath.ToSlash(specPath)
	fid := graph.NodeID("file:" + specPath)
	out := parse.Result{
		Path: specPath,
		Lang: "protobuf",
		Nodes: []graph.Node{{
			ID:   fid,
			Kind: parse.KindFile,
			Name: filepath.Base(specPath),
			Path: specPath,
		}},
	}
	if fd == nil {
		out.Normalize()
		return out
	}

	pkg := fd.Package
	if fd.Desc != nil {
		emitFromDesc(&out, fid, specPath, pkg, fd.Desc)
	} else if fd.Proto != nil {
		emitFromProto(&out, fid, specPath, pkg, fd.Proto)
	}

	out.Normalize()
	return out
}

func emitFromDesc(out *parse.Result, fid graph.NodeID, specPath, pkg string, desc protoreflect.FileDescriptor) {
	msgs := desc.Messages()
	for i := 0; i < msgs.Len(); i++ {
		emitMessage(out, fid, specPath, pkg, msgs.Get(i))
	}
	svcs := desc.Services()
	for i := 0; i < svcs.Len(); i++ {
		svc := svcs.Get(i)
		svcName := string(svc.Name())
		sid := ServiceID(specPath, svcName)
		out.Nodes = append(out.Nodes, graph.Node{
			ID:   sid,
			Kind: parse.KindService,
			Name: svcName,
			Path: specPath,
			Props: map[string]string{
				"package": pkg,
			},
		})
		out.Edges = append(out.Edges, graph.Edge{From: fid, To: sid, Type: parse.EdgeContains})

		methods := svc.Methods()
		for j := 0; j < methods.Len(); j++ {
			m := methods.Get(j)
			methodName := string(m.Name())
			sym := OperationSymbol(svcName, methodName)
			oid := OperationID(specPath, svcName, methodName)
			grpcPath := GRPCPath(pkg, svcName, methodName)
			out.Nodes = append(out.Nodes, graph.Node{
				ID:   oid,
				Kind: parse.KindOperation,
				Name: sym,
				Path: specPath,
				Props: map[string]string{
					"operation_id": methodName,
					"service":      svcName,
					"grpc_path":    grpcPath,
					"path":         grpcPath, // binder path-literal consumes
					"package":      pkg,
				},
			})
			out.Edges = append(out.Edges,
				graph.Edge{From: fid, To: oid, Type: parse.EdgeContains},
				graph.Edge{From: sid, To: oid, Type: parse.EdgeContains},
			)
		}
	}
}

func emitMessage(out *parse.Result, fid graph.NodeID, specPath, pkg string, msg protoreflect.MessageDescriptor) {
	name := trimPkgPrefix(string(msg.FullName()), pkg)
	if name == "" {
		name = string(msg.Name())
	}
	sid := SchemaID(specPath, name)
	out.Nodes = append(out.Nodes, graph.Node{
		ID:   sid,
		Kind: parse.KindSchema,
		Name: name,
		Path: specPath,
		Props: map[string]string{
			"package": pkg,
		},
	})
	out.Edges = append(out.Edges, graph.Edge{From: fid, To: sid, Type: parse.EdgeContains})

	fields := msg.Fields()
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		fname := string(f.Name())
		fsym := name + "." + fname
		fidField := FieldID(specPath, name, fname)
		props := map[string]string{
			"parent":       name,
			"field_number": strconv.Itoa(int(f.Number())),
		}
		if (f.Kind() == protoreflect.MessageKind || f.Kind() == protoreflect.GroupKind) && f.Message() != nil {
			props["proto_type"] = string(f.Message().FullName())
		} else if f.Kind() == protoreflect.EnumKind && f.Enum() != nil {
			props["proto_type"] = string(f.Enum().FullName())
		} else {
			props["proto_type"] = f.Kind().String()
		}
		out.Nodes = append(out.Nodes, graph.Node{
			ID:    fidField,
			Kind:  parse.KindField,
			Name:  fsym,
			Path:  specPath,
			Props: props,
		})
		out.Edges = append(out.Edges, graph.Edge{From: sid, To: fidField, Type: parse.EdgeContains})
	}

	nested := msg.Messages()
	for i := 0; i < nested.Len(); i++ {
		emitMessage(out, fid, specPath, pkg, nested.Get(i))
	}
}

func emitFromProto(out *parse.Result, fid graph.NodeID, specPath, pkg string, proto *descriptorpb.FileDescriptorProto) {
	for _, msg := range proto.GetMessageType() {
		emitMessageProto(out, fid, specPath, pkg, "", msg)
	}
	for _, svc := range proto.GetService() {
		if svc == nil {
			continue
		}
		svcName := svc.GetName()
		if svcName == "" {
			continue
		}
		sid := ServiceID(specPath, svcName)
		out.Nodes = append(out.Nodes, graph.Node{
			ID:   sid,
			Kind: parse.KindService,
			Name: svcName,
			Path: specPath,
			Props: map[string]string{
				"package": pkg,
			},
		})
		out.Edges = append(out.Edges, graph.Edge{From: fid, To: sid, Type: parse.EdgeContains})
		for _, m := range svc.GetMethod() {
			if m == nil {
				continue
			}
			methodName := m.GetName()
			if methodName == "" {
				continue
			}
			sym := OperationSymbol(svcName, methodName)
			oid := OperationID(specPath, svcName, methodName)
			grpcPath := GRPCPath(pkg, svcName, methodName)
			out.Nodes = append(out.Nodes, graph.Node{
				ID:   oid,
				Kind: parse.KindOperation,
				Name: sym,
				Path: specPath,
				Props: map[string]string{
					"operation_id": methodName,
					"service":      svcName,
					"grpc_path":    grpcPath,
					"path":         grpcPath,
					"package":      pkg,
				},
			})
			out.Edges = append(out.Edges,
				graph.Edge{From: fid, To: oid, Type: parse.EdgeContains},
				graph.Edge{From: sid, To: oid, Type: parse.EdgeContains},
			)
		}
	}
}

func emitMessageProto(out *parse.Result, fid graph.NodeID, specPath, pkg, parent string, msg *descriptorpb.DescriptorProto) {
	if msg == nil {
		return
	}
	name := msg.GetName()
	if name == "" {
		return
	}
	if parent != "" {
		name = parent + "." + name
	}
	sid := SchemaID(specPath, name)
	out.Nodes = append(out.Nodes, graph.Node{
		ID:   sid,
		Kind: parse.KindSchema,
		Name: name,
		Path: specPath,
		Props: map[string]string{
			"package": pkg,
		},
	})
	out.Edges = append(out.Edges, graph.Edge{From: fid, To: sid, Type: parse.EdgeContains})

	for _, f := range msg.GetField() {
		if f == nil {
			continue
		}
		fname := f.GetName()
		fsym := name + "." + fname
		fidField := FieldID(specPath, name, fname)
		props := map[string]string{
			"parent":       name,
			"field_number": strconv.Itoa(int(f.GetNumber())),
		}
		if t := f.GetTypeName(); t != "" {
			props["proto_type"] = strings.TrimPrefix(t, ".")
		} else if f.Type != nil {
			props["proto_type"] = f.GetType().String()
		}
		out.Nodes = append(out.Nodes, graph.Node{
			ID:    fidField,
			Kind:  parse.KindField,
			Name:  fsym,
			Path:  specPath,
			Props: props,
		})
		out.Edges = append(out.Edges, graph.Edge{From: sid, To: fidField, Type: parse.EdgeContains})
	}
	for _, nested := range msg.GetNestedType() {
		emitMessageProto(out, fid, specPath, pkg, name, nested)
	}
}

// ParseFile loads path and returns graph IR. includePaths are protoc import roots
// (defaults to the directory containing absPath). Relative path is used as Node.Path
// and as the compile path when non-empty.
func ParseFile(absPath, relPath string, includePaths []string) (parse.Result, error) {
	if relPath == "" {
		relPath = filepath.Base(absPath)
	}
	relPath = filepath.ToSlash(relPath)
	if len(includePaths) == 0 {
		includePaths = []string{filepath.Dir(absPath)}
	}
	fd, err := Load(absPath, relPath, LoadOptions{IncludePaths: includePaths})
	if err != nil {
		return parse.Result{}, err
	}
	return ToResult(relPath, fd), nil
}
