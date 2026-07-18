import { describe, expect, it } from 'vitest';
import { translatedSourceValue } from './source-values';

describe('translatedSourceValue', () => {
	it('uses the first retained translation for title and description', () => {
		const result = {
			source: 'dmm',
			title: '原題',
			description: '説明',
			translations: [
				{ language: 'ko', title: '한국어 제목', description: '한국어 설명' },
				{ language: 'en', title: 'English title', description: 'English description' },
			],
		};

		expect(translatedSourceValue(result, 'title')).toBe('한국어 제목');
		expect(translatedSourceValue(result, 'description')).toBe('한국어 설명');
	});

	it('falls back to the raw source field when the primary translation is empty', () => {
		const result = {
			source: 'dmm',
			title: '原題',
			description: '説明',
			translations: [{ language: 'ko', title: '', description: '' }],
		};

		expect(translatedSourceValue(result, 'title')).toBe('原題');
		expect(translatedSourceValue(result, 'description')).toBe('説明');
	});
});
