import { ApiPropertyOptional } from '@nestjs/swagger';
import { IsArray, IsObject, IsOptional, IsString } from 'class-validator';

export class PutEnvBulkDto {
  @ApiPropertyOptional({
    description: 'Variables to create or update (validated per key; values are strings)',
    example: { GREETING: 'hello', FEATURE: 'on' },
  })
  @IsOptional()
  @IsObject()
  set?: Record<string, string>;

  @ApiPropertyOptional({ description: 'Keys to delete', type: [String], example: ['OLD_KEY'] })
  @IsOptional()
  @IsArray()
  @IsString({ each: true })
  remove?: string[];
}
