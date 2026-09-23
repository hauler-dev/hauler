package archives

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"hash"
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

// redundancyCRCOffset is where the checksum sits; every header byte before it is covered by the checksum along with the payload.
const redundancyCRCOffset = redundancyHeaderSize - 4

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

// newShardCRC starts a shard checksum over h's header fields, so a damaged index, count, or size fails verification just like damaged payload.
func newShardCRC(h redundancyHeader) hash.Hash32 {
	sum := crc32.NewIEEE()
	sum.Write(h.encode()[:redundancyCRCOffset])
	return sum
}

// verifyShardChecksum reports whether the shard at path, header fields and payload together, matches h's recorded checksum.
func verifyShardChecksum(path string, h redundancyHeader) (bool, error) {
	f, err := shardPayloadReader(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	sum := newShardCRC(h)
	if _, err := io.Copy(sum, f); err != nil {
		return false, err
	}
	return sum.Sum32() == h.crc32, nil
}

// dataShardCount picks the number of data shards for an archive of archiveSize bytes so each shard file, header included, fits in maxBytes, capped at 256 (reedsolomon's own limit) and never fewer than 1.
func dataShardCount(archiveSize, maxBytes int64) (int, error) {
	if maxBytes <= redundancyHeaderSize {
		return 0, fmt.Errorf("--chunk-size must be greater than [%d] bytes when using --redundancy-percent", redundancyHeaderSize)
	}
	payload := maxBytes - redundancyHeaderSize
	n := (archiveSize + payload - 1) / payload
	if n < 1 {
		n = 1
	}
	if n > 256 {
		return 0, fmt.Errorf("archive requires [%d] chunks and exceeds the 256 chunk limit... use a larger --chunk-size", n)
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
	if totalShards > 256 {
		return nil, fmt.Errorf("archive requires [%d] chunks with recovery and exceeds the 256 chunk limit... use a larger --chunk-size or a lower --redundancy-percent", totalShards)
	}

	enc, err := reedsolomon.NewStream(dataShards, parityShards)
	if err != nil {
		return nil, fmt.Errorf("failed to create redundancy encoder: %w", err)
	}

	var setID [8]byte
	if _, err := rand.Read(setID[:]); err != nil {
		return nil, fmt.Errorf("failed to generate set id: %w", err)
	}

	// Each chunk is written in place as header then payload, with its checksum patched into the header once the payload is complete.
	finalPaths := make([]string, totalShards)
	files := make([]*os.File, totalShards)
	sums := make([]hash.Hash32, totalShards)
	cleanup := func() {
		for i, f := range files {
			if f != nil {
				f.Close()
				os.Remove(finalPaths[i])
			}
		}
	}
	shardWriters := make([]io.Writer, totalShards)
	for i := 0; i < totalShards; i++ {
		h := redundancyHeader{
			setID:        setID,
			dataShards:   uint16(dataShards),
			parityShards: uint16(parityShards),
			shardIndex:   uint16(i),
			originalSize: uint64(archiveSize),
		}
		finalPaths[i] = fmt.Sprintf("%s.%03d", archivePath, i+1)
		f, err := os.OpenFile(finalPaths[i], os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("failed to create chunk [%d]: %w", i, err)
		}
		files[i] = f
		if _, err := f.Write(h.encode()); err != nil {
			cleanup()
			return nil, fmt.Errorf("failed to write header for chunk [%d]: %w", i, err)
		}
		sums[i] = newShardCRC(h)
		shardWriters[i] = io.MultiWriter(f, sums[i])
	}

	src, err := os.Open(archivePath)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to open archive for splitting: %w", err)
	}
	splitErr := enc.Split(src, shardWriters[:dataShards], archiveSize)
	src.Close()
	if splitErr != nil {
		cleanup()
		return nil, fmt.Errorf("failed to split archive into data chunks: %w", splitErr)
	}

	dataReaders := make([]io.Reader, dataShards)
	for i := 0; i < dataShards; i++ {
		if _, err := files[i].Seek(redundancyHeaderSize, io.SeekStart); err != nil {
			cleanup()
			return nil, fmt.Errorf("failed to rewind data chunk [%d]: %w", i, err)
		}
		dataReaders[i] = files[i]
	}
	if err := enc.Encode(dataReaders, shardWriters[dataShards:]); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to generate recovery chunks: %w", err)
	}

	for i, f := range files {
		crc := make([]byte, 4)
		binary.BigEndian.PutUint32(crc, sums[i].Sum32())
		if _, err := f.WriteAt(crc, redundancyCRCOffset); err != nil {
			cleanup()
			return nil, fmt.Errorf("failed to finalize chunk [%d]: %w", i, err)
		}
	}
	for i, f := range files {
		files[i] = nil
		if err := f.Close(); err != nil {
			return nil, fmt.Errorf("failed to finalize chunk [%d]: %w", i, err)
		}
	}

	if err := os.Remove(archivePath); err != nil {
		return nil, fmt.Errorf("failed to remove original archive after splitting: %w", err)
	}
	if err := removeLeftoverChunks(ctx, archivePath, totalShards); err != nil {
		return nil, err
	}

	l.Infof("split [%s] into %d chunk(s) with %d recovery chunk(s)", filepath.Base(archivePath), totalShards, parityShards)
	l.Infof("haul can recover up to [%d] lost or corrupted chunk(s)", parityShards)
	return finalPaths, nil
}

// isRedundantChunkSet peeks at matches (a chunk set already grouped by JoinChunks) and reports whether they're redundant shards rather than plain byte-range chunks.
func isRedundantChunkSet(matches []string) (bool, error) {
	for _, m := range matches {
		_, ok, err := readShardHeader(m)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

// reconstructAndJoin reassembles the original archive in tempDir from a chunk set already identified as redundant, transparently repairing any missing or corrupted shards from parity before joining.
func reconstructAndJoin(ctx context.Context, matches []string, tempDir, joinedName string) (string, error) {
	l := log.FromContext(ctx)

	// Only checksum-verified shards are trusted, and when chunks from more than one save are mixed together the save with the most verified shards wins.
	verified := map[[8]byte]map[int]string{}
	headers := map[[8]byte]redundancyHeader{}
	for _, m := range matches {
		h, ok, err := readShardHeader(m)
		if err != nil {
			return "", fmt.Errorf("failed to read chunk header for [%s]: %w", m, err)
		}
		if !ok {
			continue
		}
		checksumOK, err := verifyShardChecksum(m, h)
		if err != nil {
			return "", fmt.Errorf("failed to verify chunk [%s]: %w", m, err)
		}
		if !checksumOK {
			l.Warnf("chunk [%s] failed checksum... treating as corrupted", filepath.Base(m))
			continue
		}
		if verified[h.setID] == nil {
			verified[h.setID] = map[int]string{}
			headers[h.setID] = h
		}
		verified[h.setID][int(h.shardIndex)] = m
	}

	var header redundancyHeader
	var present map[int]string
	for id, shards := range verified {
		if len(shards) > len(present) {
			header, present = headers[id], shards
		}
	}
	if present == nil {
		return "", fmt.Errorf("no chunk(s) passed checksum verification")
	}
	if len(verified) > 1 {
		l.Warnf("ignoring chunk(s) from [%d] other save(s)", len(verified)-1)
	}

	total := int(header.dataShards) + int(header.parityShards)
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
		f, err := shardPayloadReader(path)
		if err != nil {
			return "", fmt.Errorf("failed to open chunk [%s]: %w", path, err)
		}
		openFiles = append(openFiles, f)
		valid[i] = f
	}

	enc, err := reedsolomon.NewStream(int(header.dataShards), int(header.parityShards))
	if err != nil {
		return "", fmt.Errorf("failed to create redundancy decoder: %w", err)
	}

	// Parity only matters for repair, so a set with every data shard intact joins directly without reconstructing anything.
	dataMissing := false
	for _, i := range missing {
		if i < int(header.dataShards) {
			dataMissing = true
			break
		}
	}
	if len(missing) > 0 && !dataMissing {
		l.Infof("[%d] recovery chunk(s) lost or corrupted... no repair needed", len(missing))
	}

	if dataMissing {
		if len(missing) > int(header.parityShards) {
			return "", fmt.Errorf("[%d] of [%d] chunk(s) are lost or corrupted... only [%d] can be recovered", len(missing), total, header.parityShards)
		}
		l.Warnf("recovering [%d] of [%d] chunk(s)", len(missing), total)

		fill := make([]io.Writer, total)
		fillFiles := make(map[int]*os.File, len(missing))
		for _, i := range missing {
			p := filepath.Join(tempDir, fmt.Sprintf("%s.repaired.%03d.tmp", joinedName, i))
			f, err := os.Create(p)
			if err != nil {
				return "", fmt.Errorf("failed to create recovery buffer for chunk [%d]: %w", i, err)
			}
			defer f.Close()
			defer os.Remove(p)
			fillFiles[i] = f
			fill[i] = f
		}

		if err := enc.Reconstruct(valid, fill); err != nil {
			return "", fmt.Errorf("failed to recover chunk(s): %w", err)
		}

		for _, i := range missing {
			valid[i] = fillFiles[i]
		}

		// Reconstruct reads every present shard through to EOF, so every reader needs rewinding before it can be read again.
		rewind := func() error {
			for i, r := range valid {
				offset := int64(redundancyHeaderSize)
				if fillFiles[i] != nil {
					offset = 0
				}
				if _, err := r.(*os.File).Seek(offset, io.SeekStart); err != nil {
					return fmt.Errorf("failed to rewind chunk [%d]: %w", i, err)
				}
			}
			return nil
		}
		if err := rewind(); err != nil {
			return "", err
		}

		// The library leaves a reconstructed set unverified, so check it against parity before trusting it, as its own stream decoder example does.
		ok, err := enc.Verify(valid)
		if err != nil {
			return "", fmt.Errorf("failed to verify recovered chunk(s): %w", err)
		}
		if !ok {
			return "", fmt.Errorf("recovered chunk(s) failed verification... chunks may be from different saves")
		}
		if err := rewind(); err != nil {
			return "", err
		}
		l.Infof("successfully recovered [%d] chunk(s)", len(missing))
	}

	outPath := filepath.Join(tempDir, joinedName)
	out, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("failed to create reassembled archive: %w", err)
	}
	joinErr := enc.Join(out, valid[:header.dataShards], int64(header.originalSize))
	// Close can report the final write failing (e.g. a full disk), which would otherwise hand extraction a truncated archive.
	closeErr := out.Close()
	if joinErr != nil {
		return "", fmt.Errorf("failed to reassemble archive from chunks: %w", joinErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("failed to write reassembled archive: %w", closeErr)
	}

	return outPath, nil
}
