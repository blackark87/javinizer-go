const INTERACTIVE_SELECTOR =
	'a, button, input, select, textarea, label, [role="button"], [data-card-selection-ignore]';

type ClosestTarget = {
	closest?: (selector: string) => Element | null;
};

export function shouldToggleCardFromClick(target: EventTarget | null): boolean {
	const candidate = target as ClosestTarget | null;
	return !candidate?.closest?.(INTERACTIVE_SELECTOR);
}

export function shouldToggleCardFromKey(event: KeyboardEvent): boolean {
	return (
		event.target === event.currentTarget &&
		(event.key === 'Enter' || event.key === ' ' || event.key === 'Spacebar')
	);
}
