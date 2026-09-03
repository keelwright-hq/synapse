// Package protobuf loads Protobuf (proto3) sources and maps them into Synapse graph IR.
package protobuf

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/linker"
	"github.com/bufbuild/protocompile/parser"
	"github.com/bufbuild/protocompile/reporter"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

// protoCueRe matches Protobuf SDL cues. Requires package with trailing ';' so
// Go `package main` does not false-positive.
var protoCueRe = regexp.MustCompile(`(?m)^\s*(syntax\s*=|package\s+\w[\w.]*\s*;|import\s+"|service\s+\w|message\s+\w|enum\s+\w)`)

// LooksLikeProto reports whether data appears to be a Protobuf source file.
func LooksLikeProto(data []byte) bool {
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	return protoCueRe.Match(data)
}

// FileDesc is a loaded protobuf file usable for IR mapping.
type FileDesc struct {
	// Path is the compile/import path (slash-separated), typically the repo-relative path.
	Path string
	// Package is the protobuf package name (may be empty).
	Package string
	// Desc is a linked file descriptor when compile succeeded.
	Desc protoreflect.FileDescriptor
	// Proto is set when soft-failing to an unlinked descriptor proto.
	Proto *descriptorpb.FileDescriptorProto
}

// LoadOptions configure how .proto files are resolved.
type LoadOptions struct {
	// IncludePaths are protoc-style import roots (filesystem).
	IncludePaths []string
	// ExtraSources maps import path → file contents (used by tests and LoadBytes).
	ExtraSources map[string]string
}

// Load reads and parses a Protobuf file from absPath.
// compilePath is the path used for Compile / imports (usually the repo-relative path).
// On missing imports, falls back to a syntax-only parse of the primary file (soft-fail).
func Load(absPath, compilePath string, opts LoadOptions) (*FileDesc, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	return LoadBytes(data, compilePath, opts)
}

// LoadBytes parses Protobuf SDL from data.
func LoadBytes(data []byte, compilePath string, opts LoadOptions) (*FileDesc, error) {
	if !LooksLikeProto(data) {
		return nil, fmt.Errorf("protobuf: not a Protobuf source document")
	}
	if compilePath == "" {
		compilePath = "schema.proto"
	}
	compilePath = filepath.ToSlash(compilePath)

	files, err := compileProto(compilePath, data, opts)
	if err == nil && len(files) > 0 {
		fd := files.FindFileByPath(compilePath)
		if fd == nil {
			fd = files[0]
		}
		return &FileDesc{
			Path:    compilePath,
			Package: string(fd.Package()),
			Desc:    fd,
		}, nil
	}

	// Soft-fail: syntax-only parse of the primary file so indexing stays incremental.
	proto, perr := parseUnlinked(compilePath, data)
	if perr != nil {
		if err != nil {
			return nil, fmt.Errorf("protobuf: compile: %w (fallback parse: %v)", err, perr)
		}
		return nil, fmt.Errorf("protobuf: parse: %w", perr)
	}
	return &FileDesc{
		Path:    compilePath,
		Package: proto.GetPackage(),
		Proto:   proto,
	}, nil
}

func compileProto(compilePath string, primary []byte, opts LoadOptions) (linker.Files, error) {
	sources := map[string]string{compilePath: string(primary)}
	for k, v := range opts.ExtraSources {
		sources[filepath.ToSlash(k)] = v
	}

	mapResolver := &protocompile.SourceResolver{
		Accessor: protocompile.SourceAccessorFromMap(sources),
	}
	var resolver protocompile.Resolver = mapResolver
	if len(opts.IncludePaths) > 0 {
		resolver = protocompile.CompositeResolver{
			mapResolver,
			&protocompile.SourceResolver{ImportPaths: opts.IncludePaths},
		}
	}
	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(resolver),
	}
	return compiler.Compile(context.Background(), compilePath)
}

func parseUnlinked(filename string, data []byte) (*descriptorpb.FileDescriptorProto, error) {
	handler := reporter.NewHandler(nil)
	astFile, err := parser.Parse(filename, bytes.NewReader(data), handler)
	if err != nil {
		return nil, err
	}
	res, err := parser.ResultFromAST(astFile, true, handler)
	if err != nil {
		return nil, err
	}
	return res.FileDescriptorProto(), nil
}

// IsProto3 reports whether the file declares proto3 syntax.
func IsProto3(fd *FileDesc) bool {
	if fd == nil {
		return false
	}
	if fd.Desc != nil {
		return fd.Desc.Syntax() == protoreflect.Proto3
	}
	if fd.Proto != nil {
		return fd.Proto.GetSyntax() == "proto3" || fd.Proto.GetSyntax() == ""
	}
	return false
}

// trimPkgPrefix strips a leading package. from a full protobuf name when present.
func trimPkgPrefix(full, pkg string) string {
	if pkg == "" {
		return full
	}
	prefix := pkg + "."
	return strings.TrimPrefix(full, prefix)
}
