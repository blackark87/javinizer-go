export interface ActressName {
	first_name?: string;
	last_name?: string;
	japanese_name?: string;
}

const UNKNOWN_ACTRESS_NAME = 'Unknown';
const UNKNOWN_ACTRESS_ALIASES = new Set([
	'unknown',
	'unknown actress',
	'unknown actor',
	'미지수',
	'미상',
	'알수없음',
	'알 수 없음',
	'알 수 없는',
	'불명'
]);

export interface FormatActressNameOptions {
	firstNameOrder?: boolean;
	japaneseNames?: boolean;
}

function normalizeUnknownActressName(value: string | undefined): string {
	return (value ?? '').trim().toLowerCase().replace(/\s+/g, ' ');
}

function isUnknownActressName(value: string | undefined): boolean {
	const normalized = normalizeUnknownActressName(value);
	if (!normalized) return false;
	return UNKNOWN_ACTRESS_ALIASES.has(normalized) ||
		UNKNOWN_ACTRESS_ALIASES.has(normalized.replace(/\s+/g, ''));
}

export function isUnknownActress(actress: ActressName): boolean {
	const first = normalizeUnknownActressName(actress.first_name);
	const last = normalizeUnknownActressName(actress.last_name);
	const japanese = normalizeUnknownActressName(actress.japanese_name);

	if (japanese && !isUnknownActressName(japanese)) return false;
	if (isUnknownActressName(`${last} ${first}`) || isUnknownActressName(`${first} ${last}`)) {
		return true;
	}

	const names = [first, last, japanese].filter(Boolean);
	return names.length > 0 && names.every(isUnknownActressName);
}

// Metadata editing treats Unknown as an empty-cast placeholder, not as an
// additional performer: a known actress removes it, while an empty cast gets
// one canonical placeholder.
export function normalizeEditedActresses<T extends ActressName>(actresses: T[]): T[] {
	const known = actresses.filter((actress) => !isUnknownActress(actress));
	if (known.length > 0) return known;

	const existingUnknown = actresses.find(isUnknownActress);
	if (existingUnknown) return [existingUnknown];

	return [{
		first_name: UNKNOWN_ACTRESS_NAME,
		last_name: '',
		japanese_name: UNKNOWN_ACTRESS_NAME
	} as T];
}

export function formatActressName<T extends ActressName>(
	actress: T,
	opts?: FormatActressNameOptions
): string {
	const firstNameOrder = opts?.firstNameOrder ?? false;
	const japaneseNames = opts?.japaneseNames ?? false;

	const first = actress.first_name ?? '';
	const last = actress.last_name ?? '';

	if (japaneseNames && actress.japanese_name) {
		return actress.japanese_name;
	}

	if (first === '' && last === '') {
		if (actress.japanese_name) {
			return actress.japanese_name;
		}
		return 'Unknown';
	}

	if (firstNameOrder) {
		if (first !== '' && last !== '') {
			return `${first} ${last}`;
		}
		if (first !== '') {
			return first;
		}
		return last;
	}

	if (first !== '' && last !== '') {
		return `${last} ${first}`;
	}
	if (last !== '') {
		return last;
	}
	return first;
}
