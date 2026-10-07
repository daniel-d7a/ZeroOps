import { ApiProperty, ApiPropertyOptional } from '@nestjs/swagger';
import { Transform } from 'class-transformer';
import { IsBoolean, IsOptional, IsString, Length, Matches } from 'class-validator';
import { SLUG_PATTERN } from '../slug';

export class CreateProjectDto {
  @ApiProperty({ example: 'My First App', maxLength: 100 })
  @Transform(({ value }) => (typeof value === 'string' ? value.trim() : value))
  @IsString()
  @Length(1, 100)
  name!: string;

  @ApiPropertyOptional({ description: 'DNS label; derived from name when omitted', pattern: SLUG_PATTERN.source })
  @IsOptional()
  @Matches(SLUG_PATTERN, { message: 'slug must be a dns label: 1-63 chars, lowercase/digits/hyphens, no edge hyphens' })
  slug?: string;

  @ApiPropertyOptional({ description: 'Provision a Postgres database for this project' })
  @IsOptional()
  @IsBoolean()
  wantsDb?: boolean;
}
