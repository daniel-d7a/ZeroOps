import { Transform } from 'class-transformer';

// DTOs share one normalization so register/login/etc. can't drift apart.
export const NormalizeEmail = () =>
  Transform(({ value }) => (typeof value === 'string' ? value.trim().toLowerCase() : value));
