package archives

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"

	"github.com/klauspost/reedsolomon"
	"hauler.dev/go/hauler/v2/pkg/log"
)

// redundancyMagic identifies a hauler redundant chunk, distinguishing it from the plain byte-range chunks SplitArchive produces when no redundancy is requested.
var redundancyMagic = [8]byte{'H', 'A', 'U', 'L', 'R', 'S', 'F', '1'}

const redundancyHeaderSize = 8 + 8 + 2 + 2 + 2 + 8 + 4

// redundancyHeader is prepended to every shard file (data and parity alike) so a shard set can be reconstructed from whichever shards survive, with no separate manifest to lose or desync from the data.
type redundancyHeader struct {
	setID        [8]byte
	dataShards   uint16
	parityShards uint16
	shardIndex   uint16
	originalSize uint64
	crc32        uint32
}

func (h redundancyHeader) encode() []byte {
	buf := make([]byte, redundancyHeaderSize)
	copy(buf[0:8], redundancyMagic[:])
	copy(buf[8:16], h.setID[:])
	binary.BigEndian.PutUint16(buf[16:18], h.dataShards)
	binary.BigEndian.PutUint16(buf[18:20], h.parityShards)
	binary.BigEndian.PutUint16(buf[20:22], h.shardIndex)
	binary.BigEndian.PutUint64(buf[22:30], h.originalSize)
	binary.BigEndian.PutUint32(buf[30:34], h.crc32)
	return buf
}

// decodeRedundancyHeader parses buf as a redundancyHeader, returning ok=false (not an error) when buf doesn't start with the redundancy magic, since that just means the caller is looking at a plain, non-redundant chunk.
func decodeRedundancyHeader(buf []byte) (h redundancyHeader, ok bool) {
	if len(buf) < redundancyHeaderSize {
		return redundancyHeader{}, false
	}
	if string(buf[0:8]) != string(redundancyMagic[:]) {
		return redundancyHeader{}, false
	}
	copy(h.setID[:], buf[8:16])
	h.dataShards = binary.BigEndian.Uint16(buf[16:18])
	h.parityShards = binary.BigEndian.Uint16(buf[18:20])
	h.shardIndex = binary.BigEndian.Uint16(buf[20:22])
	h.originalSize = binary.BigEndian.Uint64(buf[22:30])
	h.crc32 = binary.BigEndian.Uint32(buf[30:34])
	return h, true
}

// readShardHeader reads and decodes just the header of the shard file at path, without reading its (potentially large) payload.
func readShardHeader(path string) (redundancyHeader, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return redundancyHeader{}, false, err
	}
	defer f.Close()

	buf := make([]byte, redundancyHeaderSize)
	if _, err := io.ReadFull(f, buf); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return redundancyHeader{}, false, nil
		}
		return redundancyHeader{}, false, err
	}
	h, ok := decodeRedundancyHeader(buf)
	return h, ok, nil
}

// shardPayloadReader opens path's shard payload, skipping past its header.
func shardPayloadReader(path string) (*os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(redundancyHeaderSize, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// verifyShardChecksum reports whether the shard payload at path matches h's recorded checksum.
func verifyShardChecksum(path string, h redundancyHeader) (bool, error) {
	f, err := shardPayloadReader(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	sum := crc32.NewIEEE()
	if _, err := io.Copy(sum, f); err != nil {
		return false, err
	}
	return sum.Sum32() == h.crc32, nil
}

// dataShardCount picks the number of data shards for an archive of archiveSize bytes given maxBytes per shard, capped at 256 (reedsolomon's own limit) and never fewer than 1.
func dataShardCount(archiveSize, maxBytes int64) (int, error) {
	if maxBytes <= 0 {
		return 0, fmt.Errorf("maxBytes must be greater than zero, received %d", maxBytes)
	}
	n := (archiveSize + maxBytes - 1) / maxBytes
	if n < 1 {
		n = 1
	}
	if n > 256 {
		return 0, fmt.Errorf("archive requires %d shards at %d bytes each, which exceeds the 256 shard limit; use a larger --chunk-size", n, maxBytes)
	}
	return int(n), nil
}

// parityShardCount derives how many parity shards to generate for dataShards at redundancyPercent, always at least 1 once redundancy is requested at all.
func parityShardCount(dataShards, redundancyPercent int) int {
	k := (dataShards*redundancyPercent + 99) / 100
	if k < 1 {
		k = 1
	}
	return k
}

// SplitArchiveRedundant splits the archive at archivePath, removing it afterward, into data and parity shards of at most maxBytes each such that any dataShards of the resulting chunks can reconstruct it.
func SplitArchiveRedundant(ctx context.Context, archivePath string, maxBytes int64, redundancyPercent int) ([]string, error) {
	l := log.FromContext(ctx)

	info, err := os.Stat(archivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat archive for splitting: %w", err)
	}
	archiveSize := info.Size()

	dataShards, err := dataShardCount(archiveSize, maxBytes)
	if err != nil {
		return nil, err
	}
	parityShards := parityShardCount(dataShards, redundancyPercent)
	totalShards := dataShards + parityShards

	enc, err := reedsolomon.NewStream(dataShards, parityShards)
	if err != nil {
		return nil, fmt.Errorf("failed to create redundancy encoder: %w", err)
	}

	var setID [8]byte
	if _, err := rand.Read(setID[:]); err != nil {
		return nil, fmt.Errorf("failed to generate set id: %w", err)
	}

	tempPaths := make([]string, totalShards)
	tempFiles := make([]*os.File, totalShards)
	cleanupTemps := func() {
		for _, f := range tempFiles {
			if f != nil {
				f.Close()
			}
		}
		for _, p := range tempPaths {
			if p != "" {
				os.Remove(p)
			}
		}
	}
	for i := 0; i < totalShards; i++ {
		p := fmt.Sprintf("%s.redundancy.%03d.tmp", archivePath, i)
		f, err := os.Create(p)
		if err != nil {
			cleanupTemps()
			return nil, fmt.Errorf("failed to create temporary shard %d: %w", i, err)
		}
		tempPaths[i] = p
		tempFiles[i] = f
	}

	src, err := os.Open(archivePath)
	if err != nil {
		cleanupTemps()
		return nil, fmt.Errorf("failed to open archive for splitting: %w", err)
	}

	dataWriters := make([]io.Writer, dataShards)
	for i := 0; i < dataShards; i++ {
		dataWriters[i] = tempFiles[i]
	}
	splitErr := enc.Split(src, dataWriters, archiveSize)
	src.Close()
	if splitErr != nil {
		cleanupTemps()
		return nil, fmt.Errorf("failed to split archive into data shards: %w", splitErr)
	}

	dataReaders := make([]io.Reader, dataShards)
	for i := 0; i < dataShards; i++ {
		if _, err := tempFiles[i].Seek(0, io.SeekStart); err != nil {
			cleanupTemps()
			return nil, fmt.Errorf("failed to rewind data shard %d: %w", i, err)
		}
		dataReaders[i] = tempFiles[i]
	}
	parityWriters := make([]io.Writer, parityShards)
	for i := 0; i < parityShards; i++ {
		parityWriters[i] = tempFiles[dataShards+i]
	}
	if err := enc.Encode(dataReaders, parityWriters); err != nil {
		cleanupTemps()
		return nil, fmt.Errorf("failed to generate parity shards: %w", err)
	}

	for _, f := range tempFiles {
		f.Close()
	}

	finalPaths := make([]string, totalShards)
	for i := 0; i < totalShards; i++ {
		finalPath := fmt.Sprintf("%s.%03d", archivePath, i+1)
		if err := writeShardWithHeader(tempPaths[i], finalPath, redundancyHeader{
			setID:        setID,
			dataShards:   uint16(dataShards),
			parityShards: uint16(parityShards),
			shardIndex:   uint16(i),
			originalSize: uint64(archiveSize),
		}); err != nil {
			cleanupTemps()
			return nil, fmt.Errorf("failed to finalize shard %d: %w", i, err)
		}
		finalPaths[i] = finalPath
	}
	cleanupTemps()

	if err := os.Remove(archivePath); err != nil {
		return nil, fmt.Errorf("failed to remove original archive after splitting: %w", err)
	}

	l.Infof("split archive [%s] into %d data and %d parity shard(s)", filepath.Base(archivePath), dataShards, parityShards)
	return finalPaths, nil
}

// writeShardWithHeader computes payloadPath's checksum, then writes h (with that checksum) followed by the payload to finalPath, removing payloadPath afterward.
func writeShardWithHeader(payloadPath, finalPath string, h redundancyHeader) error {
	payload, err := os.Open(payloadPath)
	if err != nil {
		return err
	}
	sum := crc32.NewIEEE()
	if _, err := io.Copy(sum, payload); err != nil {
		payload.Close()
		return err
	}
	h.crc32 = sum.Sum32()

	if _, err := payload.Seek(0, io.SeekStart); err != nil {
		payload.Close()
		return err
	}

	out, err := os.Create(finalPath)
	if err != nil {
		payload.Close()
		return err
	}
	if _, err := out.Write(h.encode()); err != nil {
		payload.Close()
		out.Close()
		return err
	}
	_, copyErr := io.Copy(out, payload)
	payload.Close()
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Remove(payloadPath)
}

// isRedundantChunkSet peeks at matches (a chunk set already grouped by JoinChunks) and reports the shared header if they're redundant shards rather than plain byte-range chunks.
func isRedundantChunkSet(matches []string) (redundancyHeader, bool, error) {
	for _, m := range matches {
		h, ok, err := readShardHeader(m)
		if err != nil {
			return redundancyHeader{}, false, err
		}
		if ok {
			return h, true, nil
		}
	}
	return redundancyHeader{}, false, nil
}

// reconstructAndJoin reassembles the original archive in tempDir from a chunk set already identified as redundant, transparently repairing any missing or corrupted shards from parity before joining.
func reconstructAndJoin(ctx context.Context, matches []string, tempDir, joinedName string, header redundancyHeader) (string, error) {
	l := log.FromContext(ctx)

	total := int(header.dataShards) + int(header.parityShards)
	present := map[int]string{}
	for _, m := range matches {
		h, ok, err := readShardHeader(m)
		if err != nil {
			return "", fmt.Errorf("failed to read shard header for [%s]: %w", m, err)
		}
		if !ok || h.setID != header.setID {
			continue
		}
		present[int(h.shardIndex)] = m
	}

	valid := make([]io.Reader, total)
	var openFiles []*os.File
	defer func() {
		for _, f := range openFiles {
			f.Close()
		}
	}()

	var missing []int
	for i := 0; i < total; i++ {
		path, ok := present[i]
		if !ok {
			missing = append(missing, i)
			continue
		}
		h, _, err := readShardHeader(path)
		if err != nil {
			return "", fmt.Errorf("failed to read shard header for [%s]: %w", path, err)
		}
		checksumOK, err := verifyShardChecksum(path, h)
		if err != nil {
			return "", fmt.Errorf("failed to verify shard [%s]: %w", path, err)
		}
		if !checksumOK {
			l.Warnf("shard [%s] failed checksum verification, treating as damaged", filepath.Base(path))
			missing = append(missing, i)
			continue
		}
		f, err := shardPayloadReader(path)
		if err != nil {
			return "", fmt.Errorf("failed to open shard [%s]: %w", path, err)
		}
		openFiles = append(openFiles, f)
		valid[i] = f
	}

	enc, err := reedsolomon.NewStream(int(header.dataShards), int(header.parityShards))
	if err != nil {
		return "", fmt.Errorf("failed to create redundancy decoder: %w", err)
	}

	if len(missing) > 0 {
		if len(missing) > int(header.parityShards) {
			return "", fmt.Errorf("%d of %d shard(s) are missing or damaged, more than the %d parity shard(s) available to repair with", len(missing), total, header.parityShards)
		}
		l.Warnf("repairing %d of %d shard(s) using parity", len(missing), total)

		fill := make([]io.Writer, total)
		fillFiles := make(map[int]*os.File, len(missing))
		for _, i := range missing {
			p := filepath.Join(tempDir, fmt.Sprintf("%s.repaired.%03d.tmp", joinedName, i))
			f, err := os.Create(p)
			if err != nil {
				return "", fmt.Errorf("failed to create repair buffer for shard %d: %w", i, err)
			}
			defer f.Close()
			defer os.Remove(p)
			fillFiles[i] = f
			fill[i] = f
		}

		if err := enc.Reconstruct(valid, fill); err != nil {
			return "", fmt.Errorf("failed to reconstruct missing shard(s): %w", err)
		}

		for _, i := range missing {
			f := fillFiles[i]
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return "", fmt.Errorf("failed to rewind repaired shard %d: %w", i, err)
			}
			valid[i] = f
		}

		// Reconstruct reads every present shard through to EOF, so those readers need rewinding too before Join can read them again.
		for i, r := range valid {
			if fillFiles[i] != nil {
				continue
			}
			f, ok := r.(*os.File)
			if !ok {
				continue
			}
			if _, err := f.Seek(redundancyHeaderSize, io.SeekStart); err != nil {
				return "", fmt.Errorf("failed to rewind shard %d: %w", i, err)
			}
		}
		l.Infof("successfully repaired %d shard(s) from parity", len(missing))
	}

	outPath := filepath.Join(tempDir, joinedName)
	out, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("failed to create reassembled archive: %w", err)
	}
	defer out.Close()

	if err := enc.Join(out, valid[:header.dataShards], int64(header.originalSize)); err != nil {
		return "", fmt.Errorf("failed to reassemble archive from shards: %w", err)
	}

	return outPath, nil
}
