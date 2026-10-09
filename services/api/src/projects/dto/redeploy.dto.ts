import { ApiPropertyOptional } from '@nestjs/swagger';
import { IsOptional, IsUUID } from 'class-validator';

export class RedeployDto {
  @ApiPropertyOptional({ description: 'Source to rebuild; newest non-pruned source when omitted' })
  @IsOptional()
  @IsUUID()
  sourceId?: string;
}
