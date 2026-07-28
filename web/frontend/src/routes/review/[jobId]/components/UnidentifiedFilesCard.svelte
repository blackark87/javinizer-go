<script lang="ts">
	import { AlertTriangle, Pencil, Search } from 'lucide-svelte';
	import type { FileResult } from '$lib/api/types';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { isTranslationFailure } from '$lib/utils/translation-failure';

	interface Props {
		failedResults: FileResult[];
		onSearchManually: (result: FileResult) => void;
		onEditMetadata: (result: FileResult) => void;
	}

	let { failedResults, onSearchManually, onEditMetadata }: Props = $props();

	function basename(path: string): string {
		return path.replace(/\\/g, '/').split('/').pop() ?? path;
	}
</script>

{#if failedResults.length > 0}
	<Card class="p-4">
		<div class="flex items-center gap-2 mb-3">
			<AlertTriangle class="h-4 w-4 text-warning shrink-0" />
			<p class="text-sm font-medium">
				Files Requiring Attention ({failedResults.length})
			</p>
		</div>
		<div class="space-y-2">
			{#each failedResults as result (result.file_path)}
				{@const translationFailed = isTranslationFailure(result)}
				<div class="flex items-start justify-between gap-3 rounded-md border px-3 py-2 text-sm">
					<div class="min-w-0 flex-1">
						<div class="flex items-center gap-2">
							<p class="font-mono truncate" title={result.file_path}>
								{basename(result.file_path)}
							</p>
							{#if translationFailed}
								<span class="shrink-0 rounded bg-warning/10 px-1.5 py-0.5 text-[11px] font-medium text-warning">
									Translation failed
								</span>
							{/if}
						</div>
						{#if result.error}
							<p class="text-xs text-destructive mt-0.5 truncate" title={result.error}>
								{result.error}
							</p>
						{/if}
					</div>
					{#if translationFailed}
						<Button
							variant="outline"
							size="sm"
							onclick={() => onEditMetadata(result)}
							class="shrink-0"
						>
							{#snippet children()}
								<Pencil class="h-3 w-3 mr-1" />
								Edit metadata
							{/snippet}
						</Button>
					{:else}
						<Button
							variant="outline"
							size="sm"
							onclick={() => onSearchManually(result)}
							class="shrink-0"
						>
							{#snippet children()}
								<Search class="h-3 w-3 mr-1" />
								Search manually
							{/snippet}
						</Button>
					{/if}
				</div>
			{/each}
		</div>
	</Card>
{/if}
