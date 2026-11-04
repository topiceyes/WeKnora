package service

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

// Classification errors for a resource path. A non-local provider is reported
// by the storage_provider_unsupported blocker; a local path outside the source
// workspace is an inconsistent row the move must not guess about.
var (
	errKBWorkspaceResourceNotLocal    = errors.New("resource is not on local storage")
	errKBWorkspaceResourceOutsideTree = errors.New("resource path is outside the source workspace")
)

// buildKBWorkspaceFileSet enumerates every file the KB owns. Knowledge-scoped
// directories are walked as a whole (they also cover rows written before the
// resource registry existed); resource rows contribute the shared exports/
// files. Missing resource files are reported separately: they were already
// missing before the move, so they warn instead of blocking indefinitely.
func buildKBWorkspaceFileSet(
	baseDir string, oldTenant uint64,
	resources []kbWorkspaceTransferResourceRow, knowledgeIDs []string,
) (*kbWorkspaceFileSet, error) {
	set := &kbWorkspaceFileSet{}
	seen := make(map[string]struct{}, len(resources)+len(knowledgeIDs))
	tenantDir := filepath.Join(baseDir, strconv.FormatUint(oldTenant, 10))

	for _, knowledgeID := range knowledgeIDs {
		if strings.TrimSpace(knowledgeID) == "" {
			continue
		}
		dir := filepath.Join(tenantDir, knowledgeID)
		info, err := os.Stat(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if !info.IsDir() {
			continue
		}
		walkErr := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !entry.Type().IsRegular() {
				return nil
			}
			fileInfo, err := entry.Info()
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(baseDir, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if _, ok := seen[rel]; ok {
				return nil
			}
			seen[rel] = struct{}{}
			set.rels = append(set.rels, rel)
			set.totalBytes += fileInfo.Size()
			return nil
		})
		if walkErr != nil {
			return nil, walkErr
		}
	}

	for _, row := range resources {
		rel, err := kbWorkspaceResourceRelPath(row.PhysicalPath, oldTenant)
		if err != nil {
			if errors.Is(err, errKBWorkspaceResourceNotLocal) {
				// Covered by storage_provider_unsupported.
				continue
			}
			set.outside = append(set.outside, row.PhysicalPath)
			continue
		}
		if _, ok := seen[rel]; ok {
			continue
		}
		info, err := os.Stat(filepath.Join(baseDir, filepath.FromSlash(rel)))
		if err != nil {
			if os.IsNotExist(err) {
				set.missing = append(set.missing, rel)
				continue
			}
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		seen[rel] = struct{}{}
		set.rels = append(set.rels, rel)
		set.totalBytes += info.Size()
	}

	sort.Strings(set.rels)
	sort.Strings(set.missing)
	return set, nil
}

// kbWorkspaceResourceRelPath turns a persisted physical path into a path
// relative to the storage base dir, refusing anything that is not local
// storage inside the source workspace.
func kbWorkspaceResourceRelPath(path string, oldTenant uint64) (string, error) {
	inner := path
	if _, providerPath, ok := types.ParseStorageBackendPath(path); ok {
		inner = providerPath
	}
	provider := strings.ToLower(types.ParseProviderScheme(inner))
	if provider != "local" {
		return "", fmt.Errorf("%w: %q (provider %q)", errKBWorkspaceResourceNotLocal, path, provider)
	}
	rest := strings.TrimPrefix(inner, "local://")
	tenantSegment, tail, ok := strings.Cut(rest, "/")
	if !ok || tail == "" || tenantSegment != strconv.FormatUint(oldTenant, 10) {
		return "", fmt.Errorf("%w: %q (workspace %d)", errKBWorkspaceResourceOutsideTree, path, oldTenant)
	}
	return tenantSegment + "/" + tail, nil
}

func retenantKBWorkspaceRelPath(rel string, oldTenant, newTenant uint64) (string, bool) {
	prefix := strconv.FormatUint(oldTenant, 10) + "/"
	if !strings.HasPrefix(rel, prefix) {
		return "", false
	}
	return strconv.FormatUint(newTenant, 10) + "/" + strings.TrimPrefix(rel, prefix), true
}

// copyKBWorkspaceTransferFiles copies each file without ever overwriting an
// existing destination: the caller deletes whatever it created when the move
// fails. It returns the destination-relative paths it actually created.
func copyKBWorkspaceTransferFiles(
	baseDir string, set *kbWorkspaceFileSet, oldTenant, newTenant uint64,
) ([]string, error) {
	if set == nil {
		return nil, nil
	}
	created := make([]string, 0, len(set.rels))
	for _, rel := range set.rels {
		dstRel, ok := retenantKBWorkspaceRelPath(rel, oldTenant, newTenant)
		if !ok {
			return created, fmt.Errorf("file %q is outside workspace %d", rel, oldTenant)
		}
		src, err := secutils.SafePathUnderBase(baseDir, filepath.Join(baseDir, filepath.FromSlash(rel)))
		if err != nil {
			return created, fmt.Errorf("source %q: %w", rel, err)
		}
		dst, err := secutils.SafePathUnderBase(baseDir, filepath.Join(baseDir, filepath.FromSlash(dstRel)))
		if err != nil {
			return created, fmt.Errorf("destination %q: %w", dstRel, err)
		}
		made, err := copyKBWorkspaceFileExclusive(src, dst)
		if err != nil {
			return created, err
		}
		if made {
			created = append(created, dstRel)
		}
	}
	return created, nil
}

// copyKBWorkspaceFileExclusive reports whether it created the destination. A
// same-size destination is treated as an idempotent retry and left alone;
// anything else is an error, so a move can never clobber another object.
func copyKBWorkspaceFileExclusive(src, dst string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			srcInfo, srcErr := os.Stat(src)
			dstInfo, dstErr := os.Stat(dst)
			if srcErr == nil && dstErr == nil && srcInfo.Size() == dstInfo.Size() {
				return false, nil
			}
			return false, fmt.Errorf("destination %s already exists with different content", dst)
		}
		return false, err
	}
	in, err := os.Open(src)
	if err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return false, err
	}
	_, copyErr := io.Copy(out, in)
	closeOutErr := out.Close()
	closeInErr := in.Close()
	if copyErr != nil || closeOutErr != nil || closeInErr != nil {
		_ = os.Remove(dst)
		switch {
		case copyErr != nil:
			return false, copyErr
		case closeOutErr != nil:
			return false, closeOutErr
		default:
			return false, closeInErr
		}
	}
	return true, nil
}

func removeKBWorkspaceTransferCopies(baseDir string, rels []string) {
	removeKBWorkspaceTransferFiles(baseDir, rels)
}

func removeKBWorkspaceTransferSources(baseDir string, set *kbWorkspaceFileSet) error {
	if set == nil {
		return nil
	}
	removeKBWorkspaceTransferFiles(baseDir, set.rels)
	return nil
}

func removeKBWorkspaceTransferFiles(baseDir string, rels []string) {
	dirs := make(map[string]struct{}, len(rels))
	for _, rel := range rels {
		path := filepath.Join(baseDir, filepath.FromSlash(rel))
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			continue
		}
		dirs[filepath.Dir(path)] = struct{}{}
	}
	baseDir = filepath.Clean(baseDir)
	for dir := range dirs {
		kbWorkspacePruneEmptyDirs(baseDir, filepath.Clean(dir))
	}
}

// kbWorkspacePruneEmptyDirs removes empty directories from dir upwards, never
// touching baseDir itself. os.Remove fails on non-empty directories, which is
// exactly the stop condition we want.
func kbWorkspacePruneEmptyDirs(baseDir, dir string) {
	for {
		if dir == baseDir || dir == filepath.Dir(dir) {
			return
		}
		if !strings.HasPrefix(dir, baseDir+string(filepath.Separator)) {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}
