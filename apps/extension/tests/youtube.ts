// Synthetic browse responses shaped like youtube.com's: the renderers sit where YouTube puts them, wrapped in the
// layers the parser has to see through.

export const channelId = (n: number): string => `UC${String(n).padStart(22, 'c')}`;
export const videoId = (n: number): string => `vid${String(n).padStart(8, '0')}`;

const continuationItem = (token: string) => ({
  continuationItemRenderer: { continuationEndpoint: { continuationCommand: { token, request: 'CONTINUATION_REQUEST_TYPE_BROWSE' } } },
});

const channelRenderer = (id: string) => ({
  channelRenderer: { channelId: id, title: { simpleText: `Channel ${id}` }, navigationEndpoint: { browseEndpoint: { browseId: id } } },
});

/** The first page of FEchannels: a section list holding a shelf of channels, then the continuation. */
export function channelsFirstPage(ids: string[], token?: string): unknown {
  const contents: unknown[] = [
    { itemSectionRenderer: { contents: [{ shelfRenderer: { content: { expandedShelfContentsRenderer: { items: ids.map(channelRenderer) } } } }] } },
  ];
  if (token) {
    contents.push(continuationItem(token));
  }
  return { contents: { twoColumnBrowseResultsRenderer: { tabs: [{ tabRenderer: { selected: true, content: { sectionListRenderer: { contents } } } }] } } };
}

/** A continuation page: the items arrive in an append action. */
export function continuationPage(items: unknown[], token?: string): unknown {
  return { onResponseReceivedActions: [{ appendContinuationItemsAction: { continuationItems: token ? [...items, continuationItem(token)] : items } }] };
}

export function channelsContinuation(ids: string[], token?: string): unknown {
  return continuationPage(
    [{ itemSectionRenderer: { contents: [{ shelfRenderer: { content: { expandedShelfContentsRenderer: { items: ids.map(channelRenderer) } } } }] } }],
    token,
  );
}

/** A classic history entry, optionally with its length and the red resume bar. */
export function videoRenderer(id: string, length?: string, percent?: number): unknown {
  return {
    videoRenderer: {
      videoId: id,
      title: { runs: [{ text: `Video ${id}` }] },
      ownerText: { runs: [{ text: 'Owner', navigationEndpoint: { browseEndpoint: { browseId: channelId(999) } } }] },
      ...(length ? { lengthText: { simpleText: length } } : {}),
      thumbnailOverlays: percent === undefined ? [] : [{ thumbnailOverlayResumePlaybackRenderer: { percentDurationWatched: percent } }],
    },
  };
}

/** The newer lockup layout of a history entry, with a duration badge and a progress bar. */
export function lockupVideo(id: string, badge?: string, percent?: number): unknown {
  const overlay: Record<string, unknown> = {};
  if (badge) {
    overlay.badges = [{ thumbnailBadgeViewModel: { text: badge } }];
  }
  if (percent !== undefined) {
    overlay.progressBar = { thumbnailOverlayProgressBarViewModel: { startPercent: percent } };
  }
  return {
    lockupViewModel: {
      contentId: id,
      contentType: 'LOCKUP_CONTENT_TYPE_VIDEO',
      contentImage: { thumbnailViewModel: { overlays: [{ thumbnailBottomOverlayViewModel: overlay }] } },
      metadata: { lockupMetadataViewModel: { title: { content: `Video ${id}` } } },
    },
  };
}

export function shortsShelf(ids: string[]): unknown {
  return {
    reelShelfRenderer: {
      items: ids.map((id) => ({
        shortsLockupViewModel: { entityId: `shorts-shelf-item-${id}`, onTap: { innertubeCommand: { reelWatchEndpoint: { videoId: id } } } },
      })),
    },
  };
}

/** One day of the history: the videos first, the header after them, as YouTube orders the keys. */
export function historySection(day: string | undefined, items: unknown[]): unknown {
  return { itemSectionRenderer: { contents: items, ...(day ? { header: { itemSectionHeaderRenderer: { title: { simpleText: day } } } } : {}) } };
}

export function historyFirstPage(sections: unknown[], token?: string): unknown {
  const contents = token ? [...sections, continuationItem(token)] : sections;
  return { contents: { twoColumnBrowseResultsRenderer: { tabs: [{ tabRenderer: { content: { sectionListRenderer: { contents } } } }] } } };
}
