import { describe, it, expect } from 'vitest';
import { formatActressName, isUnknownActress, normalizeEditedActresses } from './actress';
import type { ActressName } from './actress';

function makeActress(over: Partial<ActressName>): ActressName {
	return { first_name: '', last_name: '', japanese_name: '', ...over };
}

describe('formatActressName', () => {
	const cases: Array<{
		name: string;
		actress: Partial<ActressName>;
		firstNameOrder: boolean;
		want: string;
	}> = [
		{
			name: 'both names present, first-name order',
			actress: { first_name: 'Sara', last_name: 'Aoyama' },
			firstNameOrder: true,
			want: 'Sara Aoyama'
		},
		{
			name: 'both names present, last-name order',
			actress: { first_name: 'Sara', last_name: 'Aoyama' },
			firstNameOrder: false,
			want: 'Aoyama Sara'
		},
		{
			name: 'only first name, first-name order',
			actress: { first_name: 'Sara', last_name: '' },
			firstNameOrder: true,
			want: 'Sara'
		},
		{
			name: 'only first name, last-name order',
			actress: { first_name: 'Sara', last_name: '' },
			firstNameOrder: false,
			want: 'Sara'
		},
		{
			name: 'only last name, first-name order',
			actress: { first_name: '', last_name: 'Aoyama' },
			firstNameOrder: true,
			want: 'Aoyama'
		},
		{
			name: 'only last name, last-name order',
			actress: { first_name: '', last_name: 'Aoyama' },
			firstNameOrder: false,
			want: 'Aoyama'
		},
		{
			name: 'no names, japanese_name present',
			actress: { first_name: '', last_name: '', japanese_name: '青山 sarah' },
			firstNameOrder: false,
			want: '青山 sarah'
		},
		{
			name: 'no names, no japanese_name',
			actress: { first_name: '', last_name: '', japanese_name: '' },
			firstNameOrder: false,
			want: 'Unknown'
		},
		{
			name: 'no names, no japanese_name, first-name order',
			actress: { first_name: '', last_name: '', japanese_name: '' },
			firstNameOrder: true,
			want: 'Unknown'
		}
	];

	for (const tc of cases) {
		it(tc.name, () => {
			expect(formatActressName(makeActress(tc.actress), { firstNameOrder: tc.firstNameOrder })).toBe(tc.want);
		});
	}

	it('accepts Actress-like objects with extra fields', () => {
		const actress = {
			id: 1,
			first_name: 'Sara',
			last_name: 'Aoyama',
			japanese_name: '',
			thumb_url: 'http://example.com/x.jpg'
		};
		expect(formatActressName(actress, { firstNameOrder: true })).toBe('Sara Aoyama');
	});

	// japaneseNames flag — mirrors backend models.FormatActressName JapaneseNames
	// branch (internal/models/actress_format.go:27-29): when true and a
	// japanese_name is present it takes precedence over first/last ordering.
	it('japaneseNames=true prefers japanese_name over romanized', () => {
		const actress = makeActress({ first_name: 'Sara', last_name: 'Aoyama', japanese_name: '青山 sarah' });
		expect(formatActressName(actress, { firstNameOrder: true, japaneseNames: true })).toBe('青山 sarah');
		expect(formatActressName(actress, { firstNameOrder: false, japaneseNames: true })).toBe('青山 sarah');
	});

	it('japaneseNames=true with only romanized falls back to romanized (order still applies)', () => {
		expect(
			formatActressName(makeActress({ first_name: 'Sara', last_name: 'Aoyama' }), {
				firstNameOrder: true,
				japaneseNames: true
			})
		).toBe('Sara Aoyama');
		expect(
			formatActressName(makeActress({ first_name: 'Sara', last_name: 'Aoyama' }), {
				firstNameOrder: false,
				japaneseNames: true
			})
		).toBe('Aoyama Sara');
	});

	it('japaneseNames=false with both names returns romanized (unaffected)', () => {
		const actress = makeActress({ first_name: 'Sara', last_name: 'Aoyama', japanese_name: '青山 sarah' });
		expect(formatActressName(actress, { firstNameOrder: true, japaneseNames: false })).toBe('Sara Aoyama');
		expect(formatActressName(actress, { firstNameOrder: false, japaneseNames: false })).toBe('Aoyama Sara');
	});

	it('japaneseNames=true with only japanese_name returns japanese_name', () => {
		expect(
			formatActressName(makeActress({ first_name: '', last_name: '', japanese_name: '青山 sarah' }), {
				firstNameOrder: false,
				japaneseNames: true
			})
		).toBe('青山 sarah');
	});
});

describe('normalizeEditedActresses', () => {
	const unknown = {
		id: 2425,
		first_name: 'Unknown',
		last_name: '',
		japanese_name: 'Unknown'
	};
	const known = {
		id: 1,
		first_name: '유이',
		last_name: '미하마',
		japanese_name: '三浜唯'
	};

	it('removes Unknown when a known actress is added', () => {
		expect(normalizeEditedActresses([unknown, known])).toEqual([known]);
	});

	it('adds canonical Unknown when the final known actress is removed', () => {
		expect(normalizeEditedActresses([])).toEqual([
			{ first_name: 'Unknown', last_name: '', japanese_name: 'Unknown' }
		]);
	});

	it('preserves one existing Unknown record and its database identity', () => {
		expect(normalizeEditedActresses([unknown, { ...unknown }])).toEqual([unknown]);
	});

	it('does not mistake a real Japanese name with an Unknown display field for a placeholder', () => {
		const partiallyNamed = {
			first_name: 'Unknown',
			last_name: '',
			japanese_name: '三浜唯'
		};
		expect(isUnknownActress(partiallyNamed)).toBe(false);
		expect(normalizeEditedActresses([unknown, partiallyNamed])).toEqual([partiallyNamed]);
	});
});
