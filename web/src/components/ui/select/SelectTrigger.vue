<script setup lang="ts">
import type { SelectTriggerProps } from 'reka-ui'

import type { HTMLAttributes } from 'vue'
import { ChevronDownIcon } from '@lucide/vue'
import { reactiveOmit } from '@vueuse/core'
import { cn } from '@/lib/utils'
import { SelectIcon, SelectTrigger, useForwardProps } from 'reka-ui'

const props = withDefaults(
  defineProps<SelectTriggerProps & { class?: HTMLAttributes['class'], size?: 'sm' | 'default' }>(),
  { size: 'default' },
)

const delegatedProps = reactiveOmit(props, 'class', 'size')
const forwardedProps = useForwardProps(delegatedProps)
</script>

<template>
  <SelectTrigger
    data-slot="select-trigger"
    :data-size="size"
    v-bind="forwardedProps"
    :class="cn(
      `border-input bg-card data-placeholder:text-muted-foreground gap-1.5 rounded-md border py-2 pr-2 pl-3 text-sm shadow-none transition-[color,box-shadow,border-color] outline-none data-[size=default]:h-9 data-[size=sm]:h-8 *:data-[slot=select-value]:gap-1.5 [&_svg:not([class*=size-])]:size-4 relative flex w-fit items-center justify-between whitespace-nowrap focus-visible:border-ring focus-visible:ring-2 focus-visible:ring-ring/40 focus-visible:ring-offset-2 focus-visible:ring-offset-background aria-invalid:border-destructive disabled:cursor-not-allowed disabled:opacity-50 *:data-[slot=select-value]:line-clamp-1 *:data-[slot=select-value]:flex *:data-[slot=select-value]:items-center [&_svg]:pointer-events-none [&_svg]:shrink-0 after:absolute after:-inset-0.75 after:content-[''] data-[size=sm]:after:-inset-1.25`,
      props.class,
    )"
  >
    <slot />
    <SelectIcon as-child>
      <ChevronDownIcon class="text-muted-foreground size-4 pointer-events-none" />
    </SelectIcon>
  </SelectTrigger>
</template>
