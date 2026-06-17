FROM debian:bookworm-slim

# CS 1.6 (ReHLDS) 是 32-bit 程序，需要 i386 多架构支持库；
# voice_*.so 语音编解码模块依赖 mp3lame/vorbis/opus/flac/samplerate/sndfile/asound；
# engine_i486.so 链接了 SDL2（含 X11/Wayland 后端），即使 dedicated 模式也需要这些库存在才能加载。
RUN dpkg --add-architecture i386 && \
    apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        locales \
        procps \
        lib32gcc-s1 \
        lib32stdc++6 \
        libc6:i386 \
        libstdc++6:i386 \
        libgcc-s1:i386 \
        libasound2:i386 \
        libasyncns0:i386 \
        libbrotli1:i386 \
        libbsd0:i386 \
        libcap2:i386 \
        libdbus-1-3:i386 \
        libdecor-0-0:i386 \
        libdrm2:i386 \
        libexpat1:i386 \
        libffi8:i386 \
        libflac12:i386 \
        libfontconfig1:i386 \
        libfreetype6:i386 \
        libgbm1:i386 \
        libgcrypt20:i386 \
        libgpg-error0:i386 \
        liblz4-1:i386 \
        liblzma5:i386 \
        libmd0:i386 \
        libmp3lame0:i386 \
        libmpg123-0:i386 \
        libogg0:i386 \
        libopus0:i386 \
        libpng16-16:i386 \
        libpulse0:i386 \
        libsamplerate0:i386 \
        libsdl2-2.0-0:i386 \
        libsndfile1:i386 \
        libsystemd0:i386 \
        libvorbis0a:i386 \
        libvorbisenc2:i386 \
        libwayland-client0:i386 \
        libwayland-cursor0:i386 \
        libwayland-egl1:i386 \
        libwayland-server0:i386 \
        libx11-6:i386 \
        libx11-xcb1:i386 \
        libxau6:i386 \
        libxcb1:i386 \
        libxcb-render0:i386 \
        libxcb-shm0:i386 \
        libxcursor1:i386 \
        libxdmcp6:i386 \
        libxext6:i386 \
        libxfixes3:i386 \
        libxi6:i386 \
        libxkbcommon0:i386 \
        libxrandr2:i386 \
        libxrender1:i386 \
        libxss1:i386 \
        libzstd1:i386 \
        zlib1g:i386 && \
    apt-get -y autoremove && \
    rm -rf /var/lib/apt/lists/* && \
    localedef -i en_US -c -f UTF-8 -A /usr/share/locale/locale.alias en_US.UTF-8 && \
    ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone

ENV LC_ALL=en_US.UTF-8

WORKDIR /opt/rehlds

# 整个目录(含 cstrike/maps 等地图资源)打包进镜像，不使用 volume 挂载地图，
# 避免 changelevel 时因跨文件系统访问变慢
COPY . .

RUN chmod +x ./hlds_linux ./start.sh

EXPOSE 27015/udp

ENTRYPOINT ["./start.sh"]
