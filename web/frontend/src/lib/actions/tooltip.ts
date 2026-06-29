export interface TooltipOptions {
	label: string;
	side?: 'top' | 'bottom' | 'left' | 'right';
	delay?: number;
}

type Resolved = Required<TooltipOptions>;

const GAP = 8;

function resolve(opts: TooltipOptions | string): Resolved {
	if (typeof opts === 'string') return { label: opts, side: 'top', delay: 300 };
	return { label: opts.label, side: opts.side ?? 'top', delay: opts.delay ?? 300 };
}

function createTip(label: string): HTMLDivElement {
	const el = document.createElement('div');
	el.setAttribute('role', 'tooltip');
	el.textContent = label;
	el.style.cssText = [
		'position:fixed',
		'z-index:9999',
		'pointer-events:none',
		'max-width:240px',
		'padding:4px 8px',
		'border-radius:6px',
		'font-size:12px',
		'line-height:1.4',
		'font-weight:500',
		'white-space:nowrap',
		'text-align:center',
		'border:1px solid hsl(var(--border))',
		'background-color:hsl(var(--popover))',
		'color:hsl(var(--popover-foreground))',
		'box-shadow:0 4px 14px rgba(0,0,0,0.18)',
		'opacity:0',
		'transform:scale(0.96)',
		'transition:opacity 120ms ease, transform 120ms ease',
		'will-change:opacity, transform',
	].join(';');
	return el;
}

function position(el: HTMLDivElement, rect: DOMRect, side: Resolved['side']) {
	const margin = 4;
	let top = 0;
	let left = 0

	switch (side) {
		case 'top':
			top = rect.top - el.offsetHeight - GAP
			left = rect.left + rect.width / 2 - el.offsetWidth / 2
			break
		case 'bottom':
			top = rect.bottom + GAP
			left = rect.left + rect.width / 2 - el.offsetWidth / 2
			break
		case 'left':
			top = rect.top + rect.height / 2 - el.offsetHeight / 2
			left = rect.left - el.offsetWidth - GAP
			break
		case 'right':
			top = rect.top + rect.height / 2 - el.offsetHeight / 2
			left = rect.right + GAP
			break
	}

	const vw = window.innerWidth
	const vh = window.innerHeight

	if (left < margin) left = margin
	if (left + el.offsetWidth > vw - margin) left = vw - margin - el.offsetWidth
	if (top < margin) top = margin
	if (top + el.offsetHeight > vh - margin) top = vh - margin - el.offsetHeight

	el.style.left = `${Math.round(left)}px`
	el.style.top = `${Math.round(top)}px`
}

export function tooltip(node: HTMLElement, opts: TooltipOptions | string) {
	let current = resolve(opts)
	let tip: HTMLDivElement | null = null
	let timer: ReturnType<typeof setTimeout> | null = null

	function show() {
		if (tip || !current.label) return
		tip = createTip(current.label)
		document.body.appendChild(tip)
		const rect = node.getBoundingClientRect()
		position(tip, rect, current.side)
		requestAnimationFrame(() => {
			if (tip) {
				tip.style.opacity = '1'
				tip.style.transform = 'scale(1)'
			}
		})
	}

	function hide() {
		if (timer) {
			clearTimeout(timer)
			timer = null
		}
		if (tip) {
			tip.remove()
			tip = null
		}
	}

	function onEnter() {
		if (timer) clearTimeout(timer)
		timer = setTimeout(show, current.delay)
	}

	function onLeave() {
		hide()
	}

	function onUpdate() {
		if (tip) {
			const rect = node.getBoundingClientRect()
			position(tip, rect, current.side)
		}
	}

	node.addEventListener('mouseenter', onEnter)
	node.addEventListener('mouseleave', onLeave)
	node.addEventListener('focus', onEnter)
	node.addEventListener('blur', onLeave)
	window.addEventListener('scroll', onUpdate, true)
	window.addEventListener('resize', onUpdate)

	return {
		update(next: TooltipOptions | string) {
			current = resolve(next)
			if (tip) {
				tip.textContent = current.label
				onUpdate()
			}
		},
		destroy() {
			hide()
			node.removeEventListener('mouseenter', onEnter)
			node.removeEventListener('mouseleave', onLeave)
			node.removeEventListener('focus', onEnter)
			node.removeEventListener('blur', onLeave)
			window.removeEventListener('scroll', onUpdate, true)
			window.removeEventListener('resize', onUpdate)
		},
	}
}
