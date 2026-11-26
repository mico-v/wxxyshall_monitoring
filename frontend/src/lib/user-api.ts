import { ApiError } from './api';

export interface KeyboardData {
  uuid: string;
  numberKeyboard: string;
  lowerLetterKeyboard: string;
  upperLetterKeyboard: string;
  symbolKeyboard: string;
}

export interface LoginKeyboardResponse {
  session_id: string;
  expires_in: number;
  keyboard: KeyboardData;
}

export interface LoginResponse {
  access_token: string;
  expires_in: number;
  login_time: number;
  sno: string;
  source: string;
}

export interface RoomOption {
  value: string;
  name: string;
}

export interface UserReading {
  surplus_charge: number | null;
  show: Record<string, string>;
}

export interface UserRoom {
  campus: string;
  building: string;
  room: string;
  room_name: string;
  building_name: string;
  campus_name: string;
  reading: UserReading;
  amount_options: number[];
  amount_limits: { min: number; max: number };
}

export interface OrderRoom {
  campus?: string;
  building?: string;
  room?: string;
  campus_id?: string;
  building_id?: string;
  room_id?: string;
}

export interface RechargeOrder {
  order_id: string;
  tran_amount: number;
  actual_amount?: number;
  status: number;
  commit_date?: string;
  success_date?: string;
  pay_expire_date?: string;
  pay_id?: number;
  abstracts?: string;
  feeitemid?: number;
  room?: OrderRoom;
}

export interface PaymentMethod {
  pay_id: number;
  name: string;
  code: string;
  no_password: number;
  choose_account: boolean;
}

export interface PasswordMap {
  uuid: string;
  keys: number[];
}

export interface CreateOrderResponse {
  order: RechargeOrder;
  payment_method: PaymentMethod;
  amount_limits: { min: number; max: number };
}

export interface PreparePaymentResponse {
  order: RechargeOrder;
  payment_method: PaymentMethod;
  password_map: PasswordMap;
}

export async function readUserJSON<T>(response: Response): Promise<T> {
  let parsed: unknown;
  try {
    parsed = await response.json();
  } catch {
    if (!response.ok) {
      throw new ApiError(`HTTP ${response.status}`, response.status);
    }
    throw new ApiError('学校服务返回了无法解析的数据', response.status);
  }
  const body = parsed as T & { error?: string };
  if (!response.ok) {
    throw new ApiError(
      body && typeof body === 'object' && 'error' in body && typeof body.error === 'string'
        ? body.error
        : `HTTP ${response.status}`,
      response.status,
    );
  }
  if (!parsed || typeof parsed !== 'object') {
    throw new ApiError('学校服务返回了无效数据', response.status);
  }
  return body;
}

async function publicUserJSON<T>(url: string, init?: RequestInit): Promise<T> {
  return readUserJSON<T>(
    await fetch(url, {
      ...init,
      cache: 'no-store',
      headers: {
        ...(init?.headers ? Object.fromEntries(new Headers(init.headers).entries()) : {}),
      },
    }),
  );
}

export function getLoginKeyboard(): Promise<LoginKeyboardResponse> {
  return publicUserJSON<LoginKeyboardResponse>('/api/user/login/keyboard', { method: 'POST' });
}

export function loginWithMappedPassword(
  sessionId: string,
  username: string,
  mappedPassword: string,
): Promise<LoginResponse> {
  return publicUserJSON<LoginResponse>('/api/user/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      session_id: sessionId,
      username,
      mapped_password: mappedPassword,
    }),
  });
}

const numberLayout = '0123456789';
const lowerLayout = 'qwertyuiopasdfghjklzxcvbnm';
const upperLayout = 'QWERTYUIOPASDFGHJKLZXCVBNM';
const symbolLayout = '*\\-[]{}/!,<>?~&@#.:+|`%\'$;^"_';

function indexOfRune(value: string, char: string): number {
  return Array.from(value).indexOf(char);
}

function keyboardRunes(value: string): string[] {
  return Array.from(value);
}

/**
 * The school returns a shuffled character set for each keyboard category.
 * Convert the password locally; only this mapped value is sent to our server.
 */
export function mapKeyboardPassword(password: string, keyboard: KeyboardData): string {
  const numbers = keyboardRunes(keyboard.numberKeyboard);
  const lower = keyboardRunes(keyboard.lowerLetterKeyboard);
  const upper = keyboardRunes(keyboard.upperLetterKeyboard);
  const symbols = keyboardRunes(keyboard.symbolKeyboard);
  const symbolKeys = keyboardRunes(symbolLayout);

  if (numbers.length < 10 || lower.length < 26 || upper.length < 26 || symbols.length < symbolKeys.length) {
    throw new Error('学校安全键盘映射不完整');
  }

  return Array.from(password)
    .map((char) => {
      let index = -1;
      let mapping = symbols;
      if (char >= '0' && char <= '9') {
        index = indexOfRune(numberLayout, char);
        mapping = numbers;
      } else if (char >= 'a' && char <= 'z') {
        index = indexOfRune(lowerLayout, char);
        mapping = lower;
      } else if (char >= 'A' && char <= 'Z') {
        index = indexOfRune(upperLayout, char);
        mapping = upper;
      } else {
        index = symbolKeys.indexOf(char);
      }
      const encoded = index >= 0 ? mapping[index] : undefined;
      if (!encoded) throw new Error('密码包含学校安全键盘不支持的字符');
      return encoded;
    })
    .join('');
}
