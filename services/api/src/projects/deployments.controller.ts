import { BadRequestException, Controller, Get, Param, ParseUUIDPipe, Query, Req, UseGuards } from '@nestjs/common';
import { ApiTags } from '@nestjs/swagger';
import { Request } from 'express';
import { SessionGuard } from '../auth/session.guard';
import { DeploymentsService } from './deployments.service';

@ApiTags('deployments')
@Controller('deployments')
@UseGuards(SessionGuard)
export class DeploymentsController {
  constructor(private readonly deployments: DeploymentsService) {}

  @Get(':id')
  get(@Param('id', ParseUUIDPipe) id: string, @Req() req: Request) {
    return this.deployments.getDeployment(req.session.uid!, id);
  }

  /** Build logs, cursor-paginated (plan §7): poll `?after=<last id>`. */
  @Get(':id/logs')
  logs(@Param('id', ParseUUIDPipe) id: string, @Req() req: Request, @Query('after') after?: string) {
    const cursor = after === undefined ? 0 : Number(after);
    if (!Number.isInteger(cursor) || cursor < 0) {
      throw new BadRequestException('after must be a non-negative integer');
    }
    return this.deployments.listLogs(req.session.uid!, id, cursor);
  }
}
