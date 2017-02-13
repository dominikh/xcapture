# Xcapture - the command-line window recorder

Xcapture is a command-line driven X11 window recorder, outputting a
raw video stream on standard out for processing by other tools.

If you're looking for a scriptable tool that just provides you with
raw video and want to edit it in your favourite video editor or
process it with ffmpeg, this may be the tool for you.

If you're looking for an all-in-one solution that does encoding and
overlays and effects for you, you may want to look into
SimpleScreenRecorder or OBS instead.

## Usage

```
Usage of xcapture:
  -cfr
    	Use a constant frame rate
  -fps uint
    	FPS (default 30)
  -size string
    	Canvas size in the format WxH in pixels. Defaults to the initial size of the captured window
  -win int
    	Window ID
```

Xcapture expects at least the `-win` option to specify the window to
capture. The window ID may be obtained by tools such as xwininfo or
xdotool. To select a window for recording by clicking on it, you could
use the following:

```
xcapture -win $(xdotool selectwindow)
```

The `-fps` option controls the frame rate of the capture. In VFR
(variable frame rate) mode, this sets the upper limit. In CFR
(constant frame rate) mode, it sets the fixed frame rate. See
[Variable frame rate](#variable-frame-rate) for more details on VFR
and CFR. CFR mode can be enabled with the `-cfr` option.

The `-size` option sets the video size. By default, xcapture uses the
initial size of the captured window. The `-size` option can be useful
if you want to compose a small window on a larger video, especially if
you expect to enlarge the window at some point.

## Window resizing

When you resize the captured window, xcapture can't change the video
size. Instead, it will either render a portion of the window if it's
larger than the canvas, or draw the window in its original size on a
black background if it's smaller than the canvas.

## Output format

Xcapture will emit a Matroska stream containing uncompressed RGBA images.

Depending on your resolution and frame rate, this may produce a large
amount of data. For example, 1920x1080 at 60 FPS would produce nearly
500 MB per second (2073600 pixels at 4 byte each, 60 times a second).
The output format is meant as an intermediate format, to be processed
and reencoded after recording.

Depending on your available CPU power, storage capacity and bandwidth,
and workflows, you can process this data in a number of ways.

If you have very large, very fast storage (high-end SSDs or NVMe
storage), you can save the file directly:

```
xcapture [args] > output.mkv
```

Alternatively, you can compress the output with lz4 before storing it:

```
xcapture [args] | lz4 > output.mkv.lz4
```

On a modern mid-range CPU, this should be able to compress 1080p60 on
the fly using a single CPU core. A test capture of 60 FPS game footage
achieved a compression rate of 4:1.

If you have more CPU to spare, you could also reencode the stream to
H.264 on the fly, either lossy or lossless. To record the file in
H.264 lossless with the x264 codec, you can use something like the
following:

```
xcapture [args] | ffmpeg -i pipe:0 -c:v libx264 -qp 0 -preset ultrafast -f matroska pipe:1 > output.mkv
```

Note that we're outputting to stdout from ffmpeg instead of writing
directly to the file. In testing, this achieved much better
performance due to more caching and less seeking. It does, however,
prevent ffmpeg from writing cues, so you may want to post-process the
file after the recording has finished.

Testing it on the same recording as before, this achieved a
compression rate of 19:1 while still being able to maintain 60 FPS.

More complex setups are possible. For example, you could compress the
video with lz4, send it over a fast network to another machine, and
have that machine reencode the file in lossless H.264 and store it.

Finally, to test xcapture without storing any data, you can pipe it
into a media player such as ffplay:

```
xcapture [args] | ffplay -loglevel quiet -
```

### Variable frame rate

By default, xcapture emits a video with a variable frame rate, where
new frames are only written if the screen content changed (or at least
once every second). This can reduce CPU load and I/O, and allows for
much higher maximum frame rates if content is mostly static but
sometimes updates rapidly. In this mode, the `-fps` flag sets the
maximum frame rate. For example, `-fps 60` may write new frames
anywhere between once and 60 times a second.

VFR has some downsides, however: Not all software may support it
fully, and streaming a live capture may appear jerky. For example, mpv
will not update the progress bar while displaying a frame. You can use
the `-cfr` flag to force a constant frame rate if necessary.

### Converting VFR to CFR with ffmpeg

If you've recorded your screen in VFR mode but want to convert the
video to CFR, you can use ffmpeg's `-vsync` and `-r` options.

For example, given a VFR recording `screen.mkv` with a maximum frame
rate of 30 fps, you can convert it to CFR at 30 fps with the following
command:

ffmpeg -i screen.mkv -vsync cfr -r 30 screen_cfr.mkv

(Note that we're not specifying any codec, so ffmpeg will default to
lossy H.264. Extend the command as necessary).

## Status bar

Xcapture prints a simple status bar in the following form:

```
Frame time:     9.983004ms (100.17 FPS);   664 dup
```

This displays the rate at which the render loop runs, and the number
of duplicated frames.

The frame time refers to one cycle of the render loop and should
always match the frame rate set with `-fps`, even in VFR mode. If it
is slower than the targetted rate, either your system is too slow, or
the output isn't being processed fast enough, for example due to a
slow disk or a slow encoder. In order to achieve a clean recording,
processing must always keep up with recording.

The `dup` statistic tells you how many frames were duplicated, instead
of being actually captured. In VFR mode, it's perfectly fine to see a
large number of duplicates. That simply means that the screen didn't
update.

In CFR mode, however, it means that we couldn't capture the window
contents fast enough and had to emit a duplicate frame in order to
maintain a constant frame rate. Seeing a large number of dups in CFR
mode is bad and is caused by a too slow CPU. A small number of dups
can occur during window resizing or moving.
