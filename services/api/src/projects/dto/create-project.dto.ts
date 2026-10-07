import { Transform } from 'class-transformer';
import { IsBoolean, IsOptional, IsString, Length, Matches } from 'class-validator';
import { SLUG_PATTERN } from '../slug';

export class CreateProjectDto {
  @Transform(({ value }) => (typeof value === 'string' ? value.trim() : value))
  @IsString()
  @Length(1, 100)
  name!: string;

  @IsOptional()
  @Matches(SLUG_PATTERN, { message: 'slug must be a dns label: 1-63 chars, lowercase/digits/hyphens, no edge hyphens' })
  slug?: string;

  @IsOptional()
  @IsBoolean()
  wantsDb?: boolean;
}
