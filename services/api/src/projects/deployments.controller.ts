import { Controller, Get, Param, ParseUUIDPipe, Req, UseGuards } from '@nestjs/common';
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
}
