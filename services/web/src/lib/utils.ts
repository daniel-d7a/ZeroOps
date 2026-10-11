import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';

/** shadcn-vue's class helper: conditional classes + tailwind dedup. */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
