import { Body, Controller, Get, Param, ParseUUIDPipe, Patch, Post, Query, Req, UseGuards } from '@nestjs/common';
import { ApiHeader, ApiTags } from '@nestjs/swagger';
import { Request } from 'express';
import { PageQueryDto } from '../common/page-query.dto';
import { Roles } from '../common/roles.decorator';
import { RolesGuard } from '../common/roles.guard';
import { SessionGuard } from '../auth/session.guard';
import { ActionsService } from '../projects/actions.service';
import { AdminService } from './admin.service';
import { UpdateUserStatusDto } from './dto/update-user-status.dto';

@ApiTags('admin')
@Controller('admin')
@UseGuards(SessionGuard, RolesGuard)
@Roles('admin')
export class AdminController {
  constructor(
    private readonly admin: AdminService,
    private readonly actions: ActionsService,
  ) {}

  @Get('users')
  listUsers(@Query() query: PageQueryDto) {
    return this.admin.listUsers(query);
  }

  @Get('projects')
  listProjects(@Query() query: PageQueryDto) {
    return this.admin.listProjects(query);
  }

  @Post('projects/:id/stop')
  @ApiHeader({ name: 'X-CSRF-Token', required: true, description: 'csrfToken from login or /auth/me' })
  stopProject(@Param('id', ParseUUIDPipe) id: string, @Req() req: Request) {
    return this.actions.adminStop(id, req.session.uid!);
  }

  @Patch('users/:id')
  @ApiHeader({ name: 'X-CSRF-Token', required: true, description: 'csrfToken from login or /auth/me' })
  updateUser(@Param('id', ParseUUIDPipe) id: string, @Body() dto: UpdateUserStatusDto, @Req() req: Request) {
    return this.admin.updateUserStatus(id, dto.status, req.session.uid!);
  }
}
