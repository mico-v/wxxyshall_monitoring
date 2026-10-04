export interface DaysSearch {
  days?: number;
}

const ALLOWED = new Set([7, 30, 90]);

/** URL 上的时间范围：只接受 7/30/90，其它（含缺省）视为「全部」= 0。 */
export function validateDaysSearch(search: Record<string, unknown>): DaysSearch {
  const raw = Number(search.days);
  return { days: ALLOWED.has(raw) ? raw : 0 };
}
