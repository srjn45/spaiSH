package fusefs

import (
	"context"
	"os"
	"path/filepath"
	"syscall"

	"github.com/hanwen/go-fuse/v2/fs"
	libfuse "github.com/hanwen/go-fuse/v2/fuse"
)

// Root is the FUSE root inode. It lists the five operation directories.
type Root struct {
	fs.Inode
	h *Handler
}

var _ fs.NodeLookuper = (*Root)(nil)
var _ fs.NodeReaddirer = (*Root)(nil)

func (r *Root) Lookup(ctx context.Context, name string, out *libfuse.EntryOut) (*fs.Inode, syscall.Errno) {
	if !ValidOps[name] {
		return nil, syscall.ENOENT
	}
	stable := fs.StableAttr{Mode: libfuse.S_IFDIR}
	if name == "ask" {
		return r.NewInode(ctx, &AskDir{h: r.h}, stable), 0
	}
	return r.NewInode(ctx, &OpDir{op: name, realPath: "/", h: r.h}, stable), 0
}

func (r *Root) Readdir(_ context.Context) (fs.DirStream, syscall.Errno) {
	entries := []libfuse.DirEntry{
		{Name: "explain", Mode: libfuse.S_IFDIR},
		{Name: "summarise", Mode: libfuse.S_IFDIR},
		{Name: "fix", Mode: libfuse.S_IFDIR},
		{Name: "security", Mode: libfuse.S_IFDIR},
		{Name: "ask", Mode: libfuse.S_IFDIR},
	}
	return fs.NewListDirStream(entries), 0
}

// OpDir is a virtual directory that mirrors the real filesystem under one operation.
type OpDir struct {
	fs.Inode
	op       string
	realPath string // real filesystem path this node mirrors
	h        *Handler
}

var _ fs.NodeLookuper = (*OpDir)(nil)
var _ fs.NodeReaddirer = (*OpDir)(nil)

func (d *OpDir) Lookup(ctx context.Context, name string, out *libfuse.EntryOut) (*fs.Inode, syscall.Errno) {
	real := filepath.Join(d.realPath, name)
	info, err := os.Lstat(real)
	if err != nil {
		return nil, syscall.ENOENT
	}
	if info.IsDir() {
		child := &OpDir{op: d.op, realPath: real, h: d.h}
		return d.NewInode(ctx, child, fs.StableAttr{Mode: libfuse.S_IFDIR}), 0
	}
	child := &VirtualFile{op: d.op, target: real, h: d.h}
	return d.NewInode(ctx, child, fs.StableAttr{Mode: libfuse.S_IFREG}), 0
}

func (d *OpDir) Readdir(_ context.Context) (fs.DirStream, syscall.Errno) {
	entries, err := os.ReadDir(d.realPath)
	if err != nil {
		return nil, syscall.EIO
	}
	result := make([]libfuse.DirEntry, 0, len(entries))
	for _, e := range entries {
		mode := libfuse.S_IFREG
		if e.IsDir() {
			mode = libfuse.S_IFDIR
		}
		result = append(result, libfuse.DirEntry{Name: e.Name(), Mode: uint32(mode)})
	}
	return fs.NewListDirStream(result), 0
}

// AskDir is the virtual directory for free-form questions.
// Any file name under /ai/ask/ is treated as the question.
// ls /ai/ask/ returns empty — there is no real filesystem to mirror.
type AskDir struct {
	fs.Inode
	h *Handler
}

var _ fs.NodeLookuper = (*AskDir)(nil)
var _ fs.NodeReaddirer = (*AskDir)(nil)

func (d *AskDir) Lookup(ctx context.Context, name string, _ *libfuse.EntryOut) (*fs.Inode, syscall.Errno) {
	child := &VirtualFile{op: "ask", target: name, h: d.h}
	return d.NewInode(ctx, child, fs.StableAttr{Mode: libfuse.S_IFREG}), 0
}

func (d *AskDir) Readdir(_ context.Context) (fs.DirStream, syscall.Errno) {
	return fs.NewListDirStream(nil), 0
}

// VirtualFile is a readable FUSE file. Opening it triggers one AI call.
// Content is generated in Open and cached for the duration of the file descriptor.
type VirtualFile struct {
	fs.Inode
	op     string
	target string // real path for file ops; question string for "ask"
	h      *Handler
}

var _ fs.NodeOpener = (*VirtualFile)(nil)

func (f *VirtualFile) Open(ctx context.Context, _ uint32) (fs.FileHandle, uint32, syscall.Errno) {
	callerEnv := map[string]string{}
	if caller, ok := libfuse.FromContext(ctx); ok {
		callerEnv = ReadCallerEnv(caller.Pid)
	}

	pp := ParsedPath{Op: f.op, Target: f.target, IsAsk: f.op == "ask"}
	content := f.h.Call(pp, callerEnv)
	return &fileHandle{content: content}, libfuse.FOPEN_DIRECT_IO, 0
}

// fileHandle holds AI-generated content for one open file descriptor.
type fileHandle struct {
	content []byte
}

var _ fs.FileReader = (*fileHandle)(nil)

func (fh *fileHandle) Read(_ context.Context, dest []byte, off int64) (libfuse.ReadResult, syscall.Errno) {
	if int(off) >= len(fh.content) {
		return libfuse.ReadResultData(nil), 0
	}
	end := int(off) + len(dest)
	if end > len(fh.content) {
		end = len(fh.content)
	}
	return libfuse.ReadResultData(fh.content[int(off):end]), 0
}

// Mount mounts the spaiOS FUSE filesystem at mountpoint and returns the server.
// The caller must call srv.Wait() to block until unmount, or srv.Unmount() to stop.
func Mount(mountpoint string, h *Handler) (*libfuse.Server, error) {
	root := &Root{h: h}
	opts := &fs.Options{
		MountOptions: libfuse.MountOptions{
			Name:   "spai",
			FsName: "spaiOS",
			Debug:  false,
		},
	}
	return fs.Mount(mountpoint, root, opts)
}
