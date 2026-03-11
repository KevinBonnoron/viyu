/** A comment of a video's conversation, with the bounded answers the platform fetched for it. */
export interface VideoComment {
  id: string;
  author: string;
  text: string;
  publishedAt: string;
  likeCount: number;
  /** Answer threads, flat and one level deep; absent when the platform offered none. */
  replies?: VideoComment[];
}

export interface VideoCommentsPage {
  comments: VideoComment[];
}
