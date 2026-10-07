// Package pcm provides local, streaming conversion between packed, interleaved
// PCM sample representations.
//
// A Converter validates complete input/output PCMFormat descriptions and global
// options (resampling filter and requantization dither). NewStream creates an
// independent streaming session. Stream.Append accepts arbitrary byte chunk
// sizes and returns converted output as soon as it becomes available;
// Stream.Close flushes the finite resampler tail.
//
// The supported layouts are mono, stereo, 5.1 and 7.1. Supported sample
// representations are unsigned 8-bit PCM and signed 16-, 24- and 32-bit PCM in
// either byte order, plus IEEE-754 32- and 64-bit floating point in either
// byte order. Unsupported descriptions and invalid options are rejected before
// samples are processed.
//
// Conversion is deterministic: concatenating all Append and Close results is
// independent of chunk boundaries. Resampling uses documented zero-extended
// filter edges and emits ceil(inFrames*outRate/inRate) output frames. Stream
// failures are returned as *StreamError with ErrorKind, logical byte offset
// and frame location; one failed session does not affect other sessions.
package pcm
