import { IsIn } from 'class-validator';

export class UpdateUserStatusDto {
  @IsIn(['active', 'disabled'])
  status!: 'active' | 'disabled';
}
