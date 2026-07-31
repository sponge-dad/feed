// 通用约定类型（来源：docs/api-spec/README.md 统一响应结构）

/** 统一响应包裹：{ code, message, data, request_id } */
export interface ApiResponse<T = unknown> {
  code: number;
  message: string;
  data: T;
  request_id: string;
}

/** Cursor 分页通用形状（以 *.api 为准：list / next_cursor / has_more） */
export interface CursorPage<T> {
  list: T[];
  next_cursor: string;
  has_more: boolean;
}
