// 前端 mock 层：在不依赖后端的前提下跑通登录 + 信息流 + 互动全流程。
// 通过 axios adapter 拦截请求，直接返回与 *.api 契约 1:1 的「统一响应体」。
// 仅在 import.meta.env.VITE_USE_MOCK === 'true' 时启用（见 src/api/request.ts）。
//
// 注意：ID 字段为后端 snowflake int64，前端统一以 string 承载，与 src/types/* 一致。
// 本文件仅供前端自测/演示，字段以契约为准；联调时关闭即可走真实网关。

import type { AxiosAdapter, AxiosResponse } from 'axios';
import type { CommentEntry, CommentReply } from '@/types/comment';
import type { FeedAuthor, FeedAuthorDetail, FeedCard, FeedDetail } from '@/types/feed';
import type { RelationUser } from '@/types/relation';
import type { User, UserDetail } from '@/types/user';

/* ----------------------------- 工具 ----------------------------- */

const delay = (ms = 280) => new Promise((r) => setTimeout(r, ms));

function avatar(seed: string, color: string): string {
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="80" height="80"><rect width="100%" height="100%" fill="${color}"/><text x="50%" y="52%" font-size="34" font-family="sans-serif" fill="#fff" text-anchor="middle" dominant-baseline="central">${seed.slice(0, 1).toUpperCase()}</text></svg>`;
  return `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`;
}

function cover(colorA: string, colorB: string, label: string): string {
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="600" height="400"><defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="1"><stop offset="0%" stop-color="${colorA}"/><stop offset="100%" stop-color="${colorB}"/></linearGradient></defs><rect width="100%" height="100%" fill="url(#g)"/><text x="50%" y="52%" font-size="40" font-family="sans-serif" fill="rgba(255,255,255,.92)" text-anchor="middle" dominant-baseline="central">${label}</text></svg>`;
  return `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`;
}

function parseBody(raw: unknown): Record<string, unknown> {
  if (!raw) return {};
  if (typeof raw === 'string') {
    try {
      return JSON.parse(raw) as Record<string, unknown>;
    } catch {
      return {};
    }
  }
  return raw as Record<string, unknown>;
}

/* --------------------------- 内存数据 --------------------------- */

interface MockFeed {
  id: string;
  user_id: string;
  feed_type: number;
  title: string;
  description: string;
  media_urls: string[];
  cover_url: string;
  city_name: string;
  ip_location: string;
  created_at: number;
  like_count: number;
  comment_count: number;
  collect_count: number;
}

interface MockComment {
  id: string;
  feed_id: string;
  root_id: string; // '' 表示一级评论
  parent_id: string; // '' 表示一级评论
  user_id: string;
  reply_user_id: string; // '' 表示无
  content: string;
  like_count: number;
  is_liked: boolean;
  created_at: number;
  reply_count: number;
}

const PALETTE = ['#6366f1', '#ec4899', '#10b981', '#f59e0b', '#06b6d4', '#8b5cf6'];
const AVATAR_COLORS = PALETTE;

const users = new Map<string, UserDetail>();
const feeds: MockFeed[] = [];
const comments: MockComment[] = [];
let userIdSeq = 0;
let feedIdSeq = 0;
let commentIdSeq = 0;

// 当前登录用户（由 token 解析）
let currentUserId: string | null = null;

// 交互状态（按 currentUser 记录，key 为 string id）
const likedFeeds = new Set<string>();
const collectedFeeds = new Set<string>();
const followingSet = new Set<string>();
const likedComments = new Set<string>();

function addUser(p: Partial<UserDetail> & { username: string; nickname: string }): UserDetail {
  const id = String(++userIdSeq);
  const color = AVATAR_COLORS[userIdSeq % AVATAR_COLORS.length];
  const u: UserDetail = {
    id,
    username: p.username,
    nickname: p.nickname,
    avatar: avatar(p.nickname, color),
    bio: p.bio ?? '这个人很懒，什么都没写~',
    city_name: p.city_name ?? '深圳',
    following_count: p.following_count ?? 0,
    follower_count: p.follower_count ?? 0,
    feed_count: p.feed_count ?? 0,
    is_following: false,
  };
  users.set(id, u);
  return u;
}

function addFeed(p: Partial<MockFeed> & { user_id: string; title: string }): MockFeed {
  const id = String(++feedIdSeq);
  const colorA = AVATAR_COLORS[(feedIdSeq * 2) % PALETTE.length];
  const colorB = AVATAR_COLORS[(feedIdSeq * 3 + 1) % PALETTE.length];
  const f: MockFeed = {
    id,
    user_id: p.user_id,
    feed_type: p.feed_type ?? 1,
    title: p.title,
    description: p.description ?? '这是一段示例正文，用于演示信息流卡片与详情页渲染效果。',
    media_urls: p.media_urls ?? [cover(colorA, colorB, `Feed #${id}`)],
    cover_url: p.cover_url ?? cover(colorA, colorB, `Feed #${id}`),
    city_name: p.city_name ?? '深圳',
    ip_location: p.ip_location ?? '广东',
    created_at: p.created_at ?? Date.now() - feedIdSeq * 3600_000,
    like_count: p.like_count ?? Math.floor(Math.random() * 200),
    comment_count: 0,
    collect_count: p.collect_count ?? Math.floor(Math.random() * 50),
  };
  feeds.push(f);
  const owner = users.get(f.user_id);
  if (owner) owner.feed_count += 1;
  return f;
}

function addComment(p: Partial<MockComment> & { feed_id: string; user_id: string; content: string }): MockComment {
  const id = String(++commentIdSeq);
  const c: MockComment = {
    id,
    feed_id: p.feed_id,
    root_id: p.root_id ?? '',
    parent_id: p.parent_id ?? '',
    user_id: p.user_id,
    reply_user_id: p.reply_user_id ?? '',
    content: p.content,
    like_count: p.like_count ?? 0,
    is_liked: false,
    created_at: p.created_at ?? Date.now(),
    reply_count: 0,
  };
  comments.push(c);
  const feed = feeds.find((f) => f.id === c.feed_id);
  if (feed) feed.comment_count += 1;
  return c;
}

// 初始化种子数据
function seed(): void {
  if (users.size > 0) return;
  const alice = addUser({ username: 'alice', nickname: 'Alice', city_name: '深圳', bio: '前端摸鱼工程师' });
  const bob = addUser({ username: 'bob', nickname: 'Bob', city_name: '广州' });
  const carol = addUser({ username: 'carol', nickname: 'Carol', city_name: '北京' });
  const dave = addUser({ username: 'dave', nickname: 'Dave', city_name: '上海' });
  const erin = addUser({ username: 'erin', nickname: 'Erin', city_name: '杭州' });

  // alice 预置关注 bob、carol
  followingSet.add(bob.id);
  followingSet.add(carol.id);
  bob.follower_count += 1;
  carol.follower_count += 1;
  alice.following_count = 2;

  addFeed({ user_id: alice.id, title: '我在深圳的周末', feed_type: 1, description: '去海边逛了逛，风很大但很舒服。' });
  addFeed({ user_id: bob.id, title: '广州早茶探店', feed_type: 1, description: '虾饺和烧麦yyds。' });
  addFeed({ user_id: carol.id, title: '北京胡同漫步', feed_type: 2, description: '拍了一卷胶片。' });
  addFeed({ user_id: dave.id, title: '上海夜跑路线推荐', feed_type: 1, description: '滨江大道真的很赞。' });
  addFeed({ user_id: erin.id, title: '西湖边的咖啡馆', feed_type: 1, description: '安静到能听见雨声。' });
  addFeed({ user_id: alice.id, title: '今天学了个新 hook', feed_type: 1, description: 'useCursorList 写起来真香。' });

  // 评论种子
  const f1 = feeds[0];
  const c1 = addComment({ feed_id: f1.id, user_id: bob.id, content: '好羡慕，我也想去海边！' });
  addComment({ feed_id: f1.id, user_id: carol.id, content: '照片拍得真好', root_id: c1.id, parent_id: c1.id, reply_user_id: bob.id });
  c1.reply_count = 1;
}

seed();

/* --------------------------- 转换器 --------------------------- */

function toUser(u: UserDetail): User {
  return {
    id: u.id,
    username: u.username,
    nickname: u.nickname,
    avatar: u.avatar,
    bio: u.bio,
    city_name: u.city_name,
  };
}

function authorOf(userId: string): FeedAuthor {
  const u = users.get(userId)!;
  return { id: u.id, nickname: u.nickname, avatar: u.avatar };
}

function authorDetailOf(userId: string): FeedAuthorDetail {
  const u = users.get(userId)!;
  return { id: u.id, nickname: u.nickname, avatar: u.avatar, is_following: followingSet.has(u.id) };
}

function toCard(f: MockFeed): FeedCard {
  return {
    id: f.id,
    feed_type: f.feed_type,
    title: f.title,
    cover_url: f.cover_url,
    author: authorOf(f.user_id),
    stats: { like_count: f.like_count, comment_count: f.comment_count, collect_count: f.collect_count },
    interaction: { is_liked: likedFeeds.has(f.id), is_collected: collectedFeeds.has(f.id) },
    created_at: f.created_at,
  };
}

function toDetail(f: MockFeed): FeedDetail {
  return {
    id: f.id,
    feed_type: f.feed_type,
    title: f.title,
    description: f.description,
    media_urls: f.media_urls,
    cover_url: f.cover_url,
    city_name: f.city_name,
    ip_location: f.ip_location,
    created_at: f.created_at,
    author: authorDetailOf(f.user_id),
    stats: { like_count: f.like_count, comment_count: f.comment_count, collect_count: f.collect_count },
    interaction: { is_liked: likedFeeds.has(f.id), is_collected: collectedFeeds.has(f.id) },
  };
}

function toRelationUser(u: UserDetail): RelationUser {
  return {
    id: u.id,
    nickname: u.nickname,
    avatar: u.avatar,
    bio: u.bio,
    is_following: followingSet.has(u.id),
  };
}

function topComments(feedId: string): CommentEntry[] {
  return comments
    .filter((c) => c.feed_id === feedId && c.root_id === '')
    .sort((a, b) => b.like_count - a.like_count)
    .slice(0, 3)
    .map(toCommentEntry);
}

function toCommentEntry(c: MockComment): CommentEntry {
  const sub = comments.filter((r) => r.root_id === c.id).map(toReply);
  return {
    id: c.id,
    content: c.content,
    author: authorOf(c.user_id),
    like_count: c.like_count,
    is_liked: likedComments.has(c.id),
    reply_count: c.reply_count,
    created_at: c.created_at,
    sub_replies: sub,
  };
}

function toReply(c: MockComment): CommentReply {
  return {
    id: c.id,
    content: c.content,
    author: authorOf(c.user_id),
    reply_user: c.reply_user_id ? { id: c.reply_user_id, nickname: users.get(c.reply_user_id)?.nickname ?? '' } : { id: '', nickname: '' },
    like_count: c.like_count,
    is_liked: likedComments.has(c.id),
    created_at: c.created_at,
  };
}

/* --------------------------- 路由处理 --------------------------- */

function tokenFor(userId: string): string {
  return `mock.${userId}`;
}

function userIdFromConfig(headers: unknown): string {
  const h = (headers ?? {}) as Record<string, unknown>;
  const auth = (h.Authorization as string) || (h.authorization as string) || '';
  const m = auth.match(/^Bearer\s*mock\.(\w+)$/);
  if (m) return m[1];
  return currentUserId ?? '1';
}

function ok(data: unknown): AxiosResponse {
  return {
    data: { code: 0, message: 'ok', data, request_id: 'mock-' + Math.random().toString(16).slice(2) },
    status: 200,
    statusText: 'OK',
    headers: {},
    config: {} as AxiosResponse['config'],
  };
}

function handle(method: string, path: string, params: Record<string, unknown>, body: Record<string, unknown>, headers: unknown): unknown {
  const uid = userIdFromConfig(headers);

  // 公开：登录 / 注册
  if (method === 'POST' && path === '/users/login') {
    const username = String(body.username ?? '');
    let user = [...users.values()].find((u) => u.username === username);
    if (!user) user = addUser({ username, nickname: username });
    currentUserId = user.id;
    return { user: toUser(user), token: tokenFor(user.id) };
  }
  if (method === 'POST' && path === '/users/register') {
    const username = String(body.username ?? '');
    const nickname = String(body.nickname || username);
    const exists = [...users.values()].some((u) => u.username === username);
    if (exists) return { code: 10001, message: '用户名已存在' };
    const user = addUser({ username, nickname });
    currentUserId = user.id;
    return { user: toUser(user), token: tokenFor(user.id) };
  }

  // 需登录的接口
  if (path === '/users/me') {
    return users.get(uid)!;
  }
  let m = path.match(/^\/users\/(\w+)$/);
  if (m && method === 'GET') {
    const u = users.get(m[1])!;
    return { ...u, is_following: followingSet.has(u.id) };
  }
  if (path === '/users/me' && method === 'PATCH') {
    const u = users.get(uid)!;
    if (body.nickname != null) u.nickname = String(body.nickname);
    if (body.avatar != null) u.avatar = String(body.avatar);
    if (body.bio != null) u.bio = String(body.bio);
    if (body.city_name != null) u.city_name = String(body.city_name);
    return { user: toUser(u) };
  }

  // 上传凭证（mock 直接返回可访问的占位地址）
  if (path === '/upload/token' && method === 'POST') {
    const key = `mock/${Date.now()}.${body.file_ext || 'jpg'}`;
    return {
      upload_url: `https://mock-oss.example.com/${key}`,
      credentials: { tmp_secret_id: 'mock', tmp_secret_key: 'mock', session_token: 'mock', expired_time: Date.now() + 3600_000 },
      file_key: key,
      file_url: cover('#94a3b8', '#475569', 'uploaded'),
    };
  }

  // 信息流
  if (path === '/feeds/timeline' && method === 'GET') {
    const type = String(params.type || 'recommend');
    let list = feeds.slice();
    if (type === 'follow') list = list.filter((f) => followingSet.has(f.user_id));
    else if (type === 'city') list = list.filter((f) => f.city_name === users.get(uid)?.city_name);
    list = list.sort((a, b) => b.created_at - a.created_at);
    const cursor = String(params.cursor || '');
    const pageSize = Number(params.page_size || 10);
    const startIdx = cursor ? list.findIndex((f) => f.id === cursor) + 1 : 0;
    const page = list.slice(startIdx, startIdx + pageSize);
    const nextCursor = page.length ? page[page.length - 1].id : '';
    const hasMore = startIdx + pageSize < list.length;
    return { list: page.map(toCard), next_cursor: nextCursor, has_more: hasMore };
  }

  if (path === '/feeds' && method === 'POST') {
    const f = addFeed({
      user_id: uid,
      feed_type: Number(body.feed_type ?? 1),
      title: String(body.title || '无标题'),
      description: String(body.description || ''),
      media_urls: (body.media_urls as string[]) || [],
      cover_url: (body.cover_url as string) || ((body.media_urls as string[])?.[0] ?? cover('#94a3b8', '#475569', 'new')),
    });
    return { feed: toDetail(f) };
  }

  m = path.match(/^\/feeds\/(\w+)\/comments$/);
  if (m && method === 'POST') {
    const feedId = m[1];
    const rootId = String(body.root_id || '');
    const parentId = String(body.parent_id || '');
    const c = addComment({
      feed_id: feedId,
      user_id: uid,
      content: String(body.content || ''),
      root_id: rootId,
      parent_id: parentId,
      reply_user_id: String(body.reply_user_id || ''),
    });
    if (rootId !== '') {
      const root = comments.find((x) => x.id === rootId);
      if (root) root.reply_count += 1;
    }
    return {
      comment: {
        id: c.id,
        feed_id: c.feed_id,
        content: c.content,
        root_id: c.root_id,
        parent_id: c.parent_id,
        author: authorOf(c.user_id),
        reply_user: c.reply_user_id ? { id: c.reply_user_id, nickname: users.get(c.reply_user_id)?.nickname ?? '' } : { id: '', nickname: '' },
        like_count: c.like_count,
        created_at: c.created_at,
      },
    };
  }

  m = path.match(/^\/feeds\/(\w+)\/comments$/);
  if (m && method === 'GET') {
    const feedId = m[1];
    const cursor = String(params.cursor || '');
    const pageSize = Number(params.page_size || 20);
    const top = comments.filter((c) => c.feed_id === feedId && c.root_id === '').sort((a, b) => a.created_at - b.created_at);
    const startIdx = cursor ? top.findIndex((c) => c.id === cursor) + 1 : 0;
    const page = top.slice(startIdx, startIdx + pageSize);
    const nextCursor = page.length ? page[page.length - 1].id : '';
    return {
      hot_comments: topComments(feedId),
      list: page.map(toCommentEntry),
      next_cursor: nextCursor,
      has_more: startIdx + pageSize < top.length,
    };
  }

  m = path.match(/^\/comments\/(\w+)\/replies$/);
  if (m && method === 'GET') {
    const rootId = m[1];
    const list = comments.filter((c) => c.root_id === rootId).sort((a, b) => a.created_at - b.created_at).map(toReply);
    return { list, next_cursor: '', has_more: false };
  }

  m = path.match(/^\/comments\/(\w+)\/like$/);
  if (m && (method === 'POST' || method === 'DELETE')) {
    const id = m[1];
    const c = comments.find((x) => x.id === id);
    if (!c) return { code: 13001, message: '评论不存在' };
    const liked = likedComments.has(id);
    if (liked) { likedComments.delete(id); c.like_count -= 1; c.is_liked = false; }
    else { likedComments.add(id); c.like_count += 1; c.is_liked = true; }
    return { success: true, like_count: c.like_count };
  }

  m = path.match(/^\/comments\/(\w+)$/);
  if (m && method === 'DELETE') {
    const id = m[1];
    const idx = comments.findIndex((x) => x.id === id);
    if (idx >= 0) comments.splice(idx, 1);
    return { success: true };
  }

  // 点赞 / 收藏 帖子
  m = path.match(/^\/feeds\/(\w+)\/like$/);
  if (m && (method === 'POST' || method === 'DELETE')) {
    const id = m[1];
    const f = feeds.find((x) => x.id === id)!;
    const liked = likedFeeds.has(id);
    if (liked) { likedFeeds.delete(id); f.like_count -= 1; }
    else { likedFeeds.add(id); f.like_count += 1; }
    return { success: true, like_count: f.like_count };
  }

  m = path.match(/^\/feeds\/(\w+)\/collect$/);
  if (m && (method === 'POST' || method === 'DELETE')) {
    const id = m[1];
    const f = feeds.find((x) => x.id === id)!;
    const c = collectedFeeds.has(id);
    if (c) { collectedFeeds.delete(id); f.collect_count -= 1; }
    else { collectedFeeds.add(id); f.collect_count += 1; }
    return { success: true, collect_count: f.collect_count };
  }

  m = path.match(/^\/feeds\/(\w+)$/);
  if (m && method === 'GET') {
    const id = m[1];
    const f = feeds.find((x) => x.id === id);
    if (!f) return { code: 12001, message: '帖子不存在' };
    return toDetail(f);
  }
  if (m && method === 'DELETE') {
    const id = m[1];
    const idx = feeds.findIndex((x) => x.id === id);
    if (idx >= 0) feeds.splice(idx, 1);
    return { success: true };
  }

  // 用户帖子 / 我的赞 / 收藏
  m = path.match(/^\/users\/(\w+)\/feeds$/);
  if (m && method === 'GET') {
    const id = m[1];
    const list = feeds.filter((f) => f.user_id === id).sort((a, b) => b.created_at - a.created_at).map(toCard);
    return { list, next_cursor: '', has_more: false };
  }
  if (path === '/users/me/likes' && method === 'GET') {
    const list = feeds.filter((f) => likedFeeds.has(f.id)).map(toCard);
    return { list, next_cursor: '', has_more: false };
  }
  if (path === '/users/me/collects' && method === 'GET') {
    const list = feeds.filter((f) => collectedFeeds.has(f.id)).map(toCard);
    return { list, next_cursor: '', has_more: false };
  }

  // 关注关系
  if (path === '/relations/follow' && method === 'POST') {
    const tid = String(body.followee_id);
    if (tid === uid) return { code: 11001, message: '不能关注自己' };
    if (followingSet.has(tid)) return { code: 11002, message: '已经关注过了' };
    followingSet.add(tid);
    const me = users.get(uid)!;
    const t = users.get(tid);
    me.following_count += 1;
    if (t) t.follower_count += 1;
    return { success: true, follower_count: t?.follower_count ?? 0 };
  }
  if (path === '/relations/follow' && method === 'DELETE') {
    const tid = String(params.followee_id ?? body.followee_id);
    followingSet.delete(tid);
    const me = users.get(uid)!;
    const t = users.get(tid);
    me.following_count = Math.max(0, me.following_count - 1);
    if (t) t.follower_count = Math.max(0, t.follower_count - 1);
    return { success: true, follower_count: t?.follower_count ?? 0 };
  }
  if (path === '/relations/following' && method === 'GET') {
    const list = [...followingSet].map((id) => users.get(id)!).filter(Boolean).map(toRelationUser);
    return { list, page: Number(params.page || 1), page_size: Number(params.page_size || 20), total: list.length, has_more: false };
  }
  if (path === '/relations/followers' && method === 'GET') {
    const all = [...users.values()].filter((u) => u.id !== uid).map(toRelationUser);
    return { list: all, page: 1, page_size: 20, total: all.length, has_more: false };
  }
  if (path === '/relations/is-following' && method === 'GET') {
    return { is_following: followingSet.has(String(params.target_id)) };
  }

  // 兜底
  return { code: 404, message: `mock 未实现: ${method} ${path}` };
}

/* --------------------------- adapter --------------------------- */

export const mockAdapter: AxiosAdapter = async (config) => {
  await delay();
  const method = (config.method || 'get').toUpperCase();
  const path = (config.url || '').split('?')[0];
  const params = (config.params as Record<string, unknown>) || {};
  const body = parseBody(config.data);
  const headers = config.headers || {};
  const result = handle(method, path, params, body, headers);
  // 业务错误（带 code 非 0 且为对象）保持原样，让响应拦截走错误提示
  if (result && typeof result === 'object' && 'code' in (result as Record<string, unknown>) && (result as Record<string, unknown>).code !== 0) {
    return {
      data: result,
      status: 200,
      statusText: 'OK',
      headers: {},
      config: {} as AxiosResponse['config'],
    };
  }
  return ok(result);
};
