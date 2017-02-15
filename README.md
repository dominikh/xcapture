# Xcapture - the command-line window recorder

Xcapture is a command-line driven X11 window recorder, outputting a
raw video stream on standard out for processing by other tools.

If you're looking for a scriptable tool that just provides you with
raw video and want to edit it in your favourite video editor or
process it with ffmpeg, this may be the tool for you.

If you're looking for an all-in-one solution that does encoding and
overlays and effects for you, you may want to look into
SimpleScreenRecorder or OBS instead.

## Installation

```
go get -u honnef.co/go/xcapture
```

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
and transcoded after recording.

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

On a modern mid-range CPU, this should be able to compress 1080p30 on
the fly using a single CPU core. A test capture of 30 FPS game footage
achieved a compression rate of 4:1.

If you have more CPU to spare, you could also transcode the stream to
H.264 on the fly, either lossy or lossless. To record the file in
H.264 lossless with the x264 codec, you can use something like the
following:

```
xcapture [args] | ffmpeg -i pipe:0 -c:v libx264 -qp 0 -preset ultrafast -f matroska output.mkv
```

Testing it on the same recording as before, this achieved a
compression rate of 19:1 while still being able to maintain 30 FPS.

More complex setups are possible. For example, you could compress the
video with lz4, send it over a fast network to another machine, and
have that machine transcode the file in lossless H.264 and store it.

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

```
ffmpeg -i screen.mkv -vsync cfr -r 30 screen_cfr.mkv
```

(Note that we're not specifying any codec, so ffmpeg will default to
lossy H.264. Extend the command as necessary).

## Status output

Xcapture prints detailed status information during recording, looking
like this:

```
3600 frames, 0 dup, started recording 2m0.033503072s ago
capture latency min/max/avg: 1.57ms/6.29ms/3.22ms±0.47ms (100 %ile: 6.29ms)
write latency min/max/avg: 0.00ms/4.19ms/1.21ms±0.32ms (100 %ile: 4.19ms)
render loop min/max/avg: 0.00ms/4.72ms/1.29ms±0.29ms (100 %ile: 4.72ms)
Last slowdown: never (0 total)
```

The first line prints the number of frames written so far, how many
frames had to be duplicated (as opposed to captured from the screen)
and how long ago the recording began. In VFR mode, it's perfectly fine
to see a large number of duplicates. That simply means that the screen
didn't update.

In CFR mode, however, it means that we couldn't capture the window
contents fast enough and had to emit a duplicate frame in order to
maintain a constant frame rate. Seeing a large number of dups in CFR
mode is bad and is caused by a too slow CPU. A small number of dups
can occur during window resizing or moving.

The next three lines display various timing related information about
the screen capture process, the video output and the render loop,
which fetches screen captures and sends them out for writing. Each
line displays the following information: The minimum, maximum and
average (the arithmetic mean) time spent per one iteration, the
standard deviation of the average, and the percentile of loop
iterations that weren't too slow to keep up with the targeted frame
rate.

The final line shows the last time that we were too slow and couldn't
keep up with the frame rate.

Let's consider the following example, in which we were capturing 60
FPS CFR and sending it into ffmpeg:

```
2220 frames, 8 dup, started recording 37.016802689s ago
capture latency min/max/avg: 1.57ms/29.36ms/3.36ms±1.46ms (99.21875 %ile: 9.44ms)
write latency min/max/avg: 0.00ms/24.12ms/4.58ms±0.64ms (99.951171875 %ile: 13.11ms)
render loop min/max/avg: 0.00ms/24.12ms/4.61ms±0.72ms (99.90234375 %ile: 15.20ms)
Last slowdown: 36.929765031s ago (2 total)
```

It shows that we've recorded 2220 frames in 37 seconds, of which 8
were duplicated. 99.21875% of frames could be captured in time,
needing at most 9.44ms. 0.78125% of frames couldn't be captured in
time, and at worst we needed 29.36ms to capture a frame. This
corresponds with the number of duplicated frames.

Similarly, ~99.95% of all frames could be written in time. The
remaining ~0.04% were too slow, most likely because ffmpeg couldn't
keep up. And even in the 99.95% of cases, we came dangerously close to
16.6ms (the duration of one frame at 60 FPS); the average, however,
was 4.58ms with very little variance. Together with the final line,
which indicates that the last time we fell behind was right at the
beginning of the recording, we can deduce that ffmpeg was slow to
start but otherwise fast enough at processing our frames.

THe render loop shows similar statistics to the write loop, which
makes sense, as its runtime is largely dictated by how long it took to
write a frame.

Overall, the output shows us that, aside from a hiccup at the
beginning, we can record at our targeted frame rate without issues.

In contrast, the following example shows much worse performance, to
the point that the recorded video will be largely useless:

```
2875 frames, 18 dup, started recording 31.989022993s ago
capture latency min/max/avg: 2.62ms/24.12ms/4.41ms±1.09ms (99.21875 %ile: 8.91ms)
write latency min/max/avg: 0.00ms/18.87ms/8.10ms±0.92ms (96.875 %ile: 10.49ms)
render loop min/max/avg: 0.00ms/25.69ms/8.25ms±1.56ms (96.875 %ile: 11.01ms)
Last slowdown: 3.421969078s ago (81 total)
```

Over 3% of our frames couldn't be written fast enough, and we've
already had 81 slowdowns in 32 seconds, the last slowdown having
occured 3.4 seconds ago, so not near the start of the recording. We
don't seem able to record at the targeted frame rate.

## Codecs

When recording video, an important choice is that of the codec and its
settings. This document will not attempt to recommend either. Instead,
it will list possible factors that will restrict the set of suitable
choices.

Essentially, these are the variables that will determine your options:

- Resolution of the captured window
- Frame rate of the capture
- The kind of captured content (terminal window, fast paced game play,
  or anything in between)
- Lossless or lossy encoding
- Available disk space for the recording (alternatively, the length of
  the recording)
- Write and seek speeds of the storage
- Available CPU that can be used on encoding, without impacting the
  captured content

These variables can be divided into two categories: requirements and
available resources. Higher requirements will necessitate higher
resources, while fewer resources should cause you to readjust your
requirements.

It is possible to trade disk space and speed for CPU and vice versa,
but only to a certain degree. It is also possible, and usually
necessary, to trade one requirement for another. Recording high
resolutions at low frame rates and vice versa poses relatively little
issues, while recording at high resolution and high frame rates can
quickly exceed your resources.

For example, storing high-paced game footage at 1920x1080, 60 FPS and
lossless will require a tremendous amount of fast disk space, or a
tremendous amount of CPU, or a high amount of both.

Storing a 300x300 terminal window that only updates at a rate of 5
FPS, however, is virtually free and achievable with any codec on
almost any system.

While it is impossible to point at one specific combination of codec
and options and to say "This is the best choice", we'll try our best
to list some of the most viable choices. You are, however, encouraged
to do your own research if you expect the highest possible quality out
of your recordings.

On the lossless front – the preferred way of storing content that has
to be edited - viable options are HuffYUV, Ut Video, and to a degree
H.264 (with a QP of 0 and preferably the RGB color space). Depending
on the resolution and frame rate, storing the raw output might also be
an option. On Windows, MagicYUV and Lagarith might also be of
interest. FFmpeg on Linux, however, does not provide encoders for
these.

On the lossy front – handy for quick screencasts or capturing funny
moments – H.264 with the best settings that your CPU is capable of is
your best bet.

