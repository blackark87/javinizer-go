import type { ScraperResult } from '$lib/api/types';

export function translatedSourceValue(
	result: ScraperResult,
	field: 'title' | 'description',
): string {
	const translated = result.translations?.[0]?.[field]?.trim();
	if (translated) return translated;
	return result[field] ?? '';
}
