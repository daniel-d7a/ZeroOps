import { ApiProperty } from '@nestjs/swagger';
import { IsString, Length } from 'class-validator';

export class PutEnvDto {
  @ApiProperty({ example: 'hello world', maxLength: 4096 })
  @IsString()
  @Length(1, 4096)
  value!: string;
}
