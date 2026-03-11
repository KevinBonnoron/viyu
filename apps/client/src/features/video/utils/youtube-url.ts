export function youtubeVideoIdFromUrl(input: string): string | null {
  const value = input.trim();
  if (!/^(?:https?:\/\/)?(?:www\.|m\.|music\.)?(?:youtube\.com|youtu\.be|youtube-nocookie\.com)\//iu.test(value)) {
    return null;
  }
  try {
    const address = new URL(/^https?:\/\//iu.test(value) ? value : `https://${value}`);
    if (address.username || address.password || address.port) {
      return null;
    }
    const host = address.hostname.toLowerCase();
    const parts = address.pathname.split('/').filter(Boolean);
    let id: string | null = null;
    if (host === 'youtu.be' || host === 'www.youtu.be') {
      if (parts.length === 1) {
        id = parts[0];
      }
    } else if (['youtube.com', 'www.youtube.com', 'm.youtube.com', 'music.youtube.com', 'youtube-nocookie.com', 'www.youtube-nocookie.com'].includes(host)) {
      if (address.pathname === '/watch') {
        id = address.searchParams.get('v');
      } else if (parts.length === 2 && ['shorts', 'live', 'embed'].includes(parts[0])) {
        id = parts[1];
      }
    }
    return id && /^[A-Za-z0-9_-]{11}$/u.test(id) ? id : null;
  } catch {
    return null;
  }
}
